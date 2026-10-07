// Company sign-in with OpenID Connect (Authorization Code + PKCE), used with
// Microsoft Entra ID. See docs/entra-setup.md for the app registration.
import { UserManager, WebStorageStateStore, type User } from "oidc-client-ts";
import type { Session } from "./api";

export interface OidcSettings {
  /** Issuer, e.g. https://login.microsoftonline.com/<tenant-id>/v2.0 */
  authority: string;
  /** The app registration's Application (client) ID. */
  clientId: string;
  /** Scopes, including the studio API's scope, e.g. "openid profile offline_access api://<client-id>/access_as_user". */
  scope: string;
}

/** Reads the settings from the build environment; null means company sign-in is not configured. */
export function oidcSettings(env: ImportMetaEnv = import.meta.env): OidcSettings | null {
  const authority = env.VITE_OIDC_AUTHORITY?.trim();
  const clientId = env.VITE_OIDC_CLIENT_ID?.trim();
  if (!authority || !clientId) return null;
  return { authority, clientId, scope: env.VITE_OIDC_SCOPE?.trim() || "openid profile offline_access" };
}

export interface CompanySignIn {
  /** Sends the browser to the identity provider. */
  start(): Promise<void>;
  /** Finishes a sign-in when the page is the provider's redirect back; otherwise returns null. */
  complete(): Promise<Session | null>;
  /** Returns the session already signed in this tab, renewing it if needed. */
  restore(): Promise<Session | null>;
  /** Forgets the session in this tab. */
  signOut(): Promise<void>;
  /** Calls back with the new session whenever the access token is renewed. Returns an unsubscribe function. */
  onRenewed(callback: (session: Session) => void): () => void;
}

function toSession(user: User): Session {
  const profile = user.profile;
  const name = (profile.preferred_username as string | undefined) ?? profile.name ?? profile.sub;
  return { token: user.access_token, subject: name };
}

export function createCompanySignIn(settings: OidcSettings): CompanySignIn {
  const redirect = `${window.location.origin}/`;
  const manager = new UserManager({
    authority: settings.authority,
    client_id: settings.clientId,
    redirect_uri: redirect,
    post_logout_redirect_uri: redirect,
    response_type: "code",
    scope: settings.scope,
    // The session lasts as long as the browser tab, like the development sign-in.
    userStore: new WebStorageStateStore({ store: window.sessionStorage }),
    stateStore: new WebStorageStateStore({ store: window.sessionStorage }),
    // Renews the access token before it expires, with the refresh token from offline_access.
    automaticSilentRenew: true,
  });

  // A sign-in code can be redeemed only once, but React runs startup effects twice in
  // development, so every caller shares the first attempt.
  let completing: Promise<Session | null> | null = null;

  return {
    start: () => manager.signinRedirect(),

    complete() {
      completing ??= (async () => {
        const params = new URLSearchParams(window.location.search);
        if (!params.has("state") || !(params.has("code") || params.has("error"))) return null;
        try {
          return toSession(await manager.signinRedirectCallback());
        } finally {
          // Remove the one-time code from the address bar and history.
          window.history.replaceState(null, "", window.location.pathname);
        }
      })();
      return completing;
    },

    async restore() {
      const user = await manager.getUser();
      if (!user) return null;
      if (!user.expired) return toSession(user);
      try {
        const renewed = await manager.signinSilent();
        if (renewed) return toSession(renewed);
      } catch {
        // Renewal failed (refresh token expired or revoked): sign in again.
      }
      await manager.removeUser();
      return null;
    },

    signOut: () => manager.removeUser(),

    onRenewed(callback) {
      const handler = (user: User) => callback(toSession(user));
      manager.events.addUserLoaded(handler);
      return () => manager.events.removeUserLoaded(handler);
    },
  };
}
