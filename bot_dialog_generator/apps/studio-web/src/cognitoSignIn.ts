// Sign-in with an Amazon Cognito user pool by username and password (USER_PASSWORD_AUTH).
//
// This is for the local AWS Emulator, whose user pools have no hosted sign-in page: the
// Vite dev server forwards VITE_COGNITO_ENDPOINT (/__cognito) to the emulator. With a
// real user pool, use Cognito's managed login through the OIDC settings (VITE_OIDC_*)
// instead, so passwords are only ever typed on Cognito's own page.
// See docs/cognito-setup.md.
import type { Session } from "./api";

export interface CognitoSettings {
  /** The app client ID printed by tools/cognito-local. */
  clientId: string;
  /** Where InitiateAuth is sent: /__cognito with the dev server's proxy. */
  endpoint: string;
}

export function cognitoSettings(env: ImportMetaEnv = import.meta.env): CognitoSettings | null {
  const clientId = env.VITE_COGNITO_CLIENT_ID?.trim();
  const endpoint = env.VITE_COGNITO_ENDPOINT?.trim();
  if (!clientId || !endpoint) return null;
  return { clientId, endpoint };
}

/** Kept for the browser tab only, like every other sign-in in the studio. */
export const COGNITO_SESSION_KEY = "bdg.cognito_session";

interface StoredSession {
  username: string;
  accessToken: string;
  refreshToken: string;
  /** Milliseconds since the epoch. */
  expiresAt: number;
}

/** Renew this long before the access token expires. */
const RENEW_EARLY_MS = 60_000;

export class CognitoError extends Error {}

const UNREACHABLE =
  "Can't reach Cognito. Start the AWS Emulator, then restart the studio with npm run dev:cognito (the dev server reads the emulator's certificate when it starts).";

/** What people see. Never says whether the username exists. */
function explain(type: string | undefined): string {
  switch (type) {
    case "NotAuthorizedException":
    case "UserNotFoundException":
      return "Incorrect username or password.";
    case "PasswordResetRequiredException":
      return "This account needs a new password. Ask IT to reset it.";
    case "UserNotConfirmedException":
      return "This account is not active yet. Ask IT to confirm it.";
    case "TooManyRequestsException":
    case "LimitExceededException":
      return "Too many attempts. Wait a minute and try again.";
    default:
      return "Sign-in failed. Try again, or ask IT for help.";
  }
}

export interface CognitoSignIn {
  signIn(username: string, password: string): Promise<Session>;
  /** The session already signed in this tab, renewed if it has expired. */
  restore(): Promise<Session | null>;
  signOut(): void;
  /** Calls back with the new session whenever the access token is renewed. */
  onRenewed(callback: (session: Session) => void): () => void;
}

export function createCognitoSignIn(settings: CognitoSettings, fetchImpl: typeof fetch = (...a) => fetch(...a)): CognitoSignIn {
  const listeners = new Set<(session: Session) => void>();
  let timer: ReturnType<typeof setTimeout> | undefined;

  const load = (): StoredSession | null => {
    try {
      const raw = sessionStorage.getItem(COGNITO_SESSION_KEY);
      return raw ? (JSON.parse(raw) as StoredSession) : null;
    } catch {
      return null;
    }
  };
  const store = (s: StoredSession | null) => {
    try {
      if (s) sessionStorage.setItem(COGNITO_SESSION_KEY, JSON.stringify(s));
      else sessionStorage.removeItem(COGNITO_SESSION_KEY);
    } catch {
      // Storage blocked: the session lasts until the page reloads.
    }
  };
  const toSession = (s: StoredSession): Session => ({ token: s.accessToken, subject: s.username });

  async function initiateAuth(flow: "USER_PASSWORD_AUTH" | "REFRESH_TOKEN_AUTH", parameters: Record<string, string>) {
    let res: Response;
    try {
      res = await fetchImpl(settings.endpoint, {
        method: "POST",
        headers: { "Content-Type": "application/x-amz-json-1.1", "X-Amz-Target": "AWSCognitoIdentityProviderService.InitiateAuth" },
        body: JSON.stringify({ ClientId: settings.clientId, AuthFlow: flow, AuthParameters: parameters }),
      });
    } catch {
      throw new CognitoError(UNREACHABLE);
    }
    const body = await res.json().catch(() => ({}));
    if (!res.ok) {
      const type = String(body.__type ?? "").split("#").pop();
      // No Cognito error type: the request never reached Cognito (the dev server answered).
      if (!type || type === "UnknownService") throw new CognitoError(UNREACHABLE);
      throw new CognitoError(explain(type));
    }
    if (body.ChallengeName) {
      // The bootstrap tool sets permanent passwords; anything else needs IT.
      throw new CognitoError("This account must finish setting up (for example, choose a new password). Ask IT for help.");
    }
    const result = body.AuthenticationResult;
    if (!result?.AccessToken) throw new CognitoError(explain(undefined));
    return result as { AccessToken: string; RefreshToken?: string; ExpiresIn: number };
  }

  function schedule(s: StoredSession) {
    clearTimeout(timer);
    const wait = Math.max(s.expiresAt - Date.now() - RENEW_EARLY_MS, 0);
    timer = setTimeout(() => {
      renew(s)
        .then((next) => listeners.forEach((cb) => cb(toSession(next))))
        .catch(() => {
          // Renewal failed (refresh token expired or revoked): the next API call answers
          // 401 and the studio returns to the sign-in page.
        });
    }, wait);
  }

  async function renew(s: StoredSession): Promise<StoredSession> {
    const result = await initiateAuth("REFRESH_TOKEN_AUTH", { REFRESH_TOKEN: s.refreshToken });
    const next: StoredSession = {
      ...s,
      accessToken: result.AccessToken,
      refreshToken: result.RefreshToken ?? s.refreshToken,
      expiresAt: Date.now() + result.ExpiresIn * 1000,
    };
    store(next);
    schedule(next);
    return next;
  }

  return {
    async signIn(username, password) {
      const name = username.trim();
      const result = await initiateAuth("USER_PASSWORD_AUTH", { USERNAME: name, PASSWORD: password });
      const s: StoredSession = {
        username: name,
        accessToken: result.AccessToken,
        refreshToken: result.RefreshToken ?? "",
        expiresAt: Date.now() + result.ExpiresIn * 1000,
      };
      store(s);
      schedule(s);
      return toSession(s);
    },

    async restore() {
      const s = load();
      if (!s) return null;
      if (s.expiresAt - Date.now() > RENEW_EARLY_MS) {
        schedule(s);
        return toSession(s);
      }
      try {
        return toSession(await renew(s));
      } catch {
        store(null);
        return null;
      }
    },

    signOut() {
      clearTimeout(timer);
      store(null);
    },

    onRenewed(callback) {
      listeners.add(callback);
      return () => listeners.delete(callback);
    },
  };
}
