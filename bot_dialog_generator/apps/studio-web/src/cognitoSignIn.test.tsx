import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import cookiesDoc from "../../../docs/legal/COOKIES_AND_STORAGE.md?raw";
import { App } from "./App";
import { COGNITO_SESSION_KEY, cognitoSettings, createCognitoSignIn } from "./cognitoSignIn";
import { alice } from "./test/fixtures";

const settings = { clientId: "studio-client", endpoint: "/__cognito" };

/** A fake InitiateAuth endpoint that knows one user. */
function fakeCognito({ expiresIn = 3600 } = {}) {
  const calls: Array<{ flow: string; params: Record<string, string>; headers: Record<string, string> }> = [];
  let issued = 0;
  const fetchImpl = vi.fn(async (_url: RequestInfo | URL, init?: RequestInit) => {
    const body = JSON.parse(String(init?.body));
    calls.push({ flow: body.AuthFlow, params: body.AuthParameters, headers: init?.headers as Record<string, string> });
    const ok =
      (body.AuthFlow === "USER_PASSWORD_AUTH" && body.AuthParameters.USERNAME === "alice" && body.AuthParameters.PASSWORD === "Demo-pass-2026") ||
      (body.AuthFlow === "REFRESH_TOKEN_AUTH" && body.AuthParameters.REFRESH_TOKEN === "refresh-1");
    if (!ok) return Response.json({ __type: "NotAuthorizedException", message: "Incorrect username or password." }, { status: 400 });
    issued++;
    return Response.json({
      AuthenticationResult: {
        AccessToken: `access-${issued}`,
        IdToken: `id-${issued}`,
        ExpiresIn: expiresIn,
        TokenType: "Bearer",
        ...(body.AuthFlow === "USER_PASSWORD_AUTH" ? { RefreshToken: "refresh-1" } : {}),
      },
    });
  });
  return { fetchImpl, calls };
}

beforeEach(() => {
  sessionStorage.clear();
  localStorage.clear();
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("Cognito sign-in client", () => {
  it("is configured only when both settings are set", () => {
    expect(cognitoSettings({} as ImportMetaEnv)).toBeNull();
    expect(cognitoSettings({ VITE_COGNITO_CLIENT_ID: "c" } as ImportMetaEnv)).toBeNull();
    expect(cognitoSettings({ VITE_COGNITO_CLIENT_ID: " c ", VITE_COGNITO_ENDPOINT: "/__cognito" } as ImportMetaEnv)).toEqual({ clientId: "c", endpoint: "/__cognito" });
  });

  it("signs in with InitiateAuth and uses the access token, never the ID token", async () => {
    const { fetchImpl, calls } = fakeCognito();
    const session = await createCognitoSignIn(settings, fetchImpl).signIn(" alice ", "Demo-pass-2026");
    expect(session).toEqual({ token: "access-1", subject: "alice" });
    expect(calls[0]).toMatchObject({
      flow: "USER_PASSWORD_AUTH",
      params: { USERNAME: "alice", PASSWORD: "Demo-pass-2026" },
      headers: { "X-Amz-Target": "AWSCognitoIdentityProviderService.InitiateAuth", "Content-Type": "application/x-amz-json-1.1" },
    });
    // The session is kept for the tab, without the password or the ID token.
    const stored = sessionStorage.getItem(COGNITO_SESSION_KEY)!;
    expect(stored).not.toContain("Demo-pass-2026");
    expect(stored).not.toContain("id-1");
    expect(Object.keys(localStorage)).toEqual([]);
  });

  it("says the same thing for a wrong password and an unknown user", async () => {
    const { fetchImpl } = fakeCognito();
    const client = createCognitoSignIn(settings, fetchImpl);
    await expect(client.signIn("alice", "wrong")).rejects.toThrow("Incorrect username or password.");
    await expect(client.signIn("nobody", "Demo-pass-2026")).rejects.toThrow("Incorrect username or password.");
    expect(sessionStorage.getItem(COGNITO_SESSION_KEY)).toBeNull();
  });

  it("explains when the emulator cannot be reached", async () => {
    const client = createCognitoSignIn(settings, vi.fn(async () => Promise.reject(new TypeError("fetch failed"))));
    await expect(client.signIn("alice", "x")).rejects.toThrow(/Can't reach Cognito/);
  });

  it("restores the tab's session, and renews it with the refresh token when it has expired", async () => {
    const { fetchImpl, calls } = fakeCognito({ expiresIn: 30 }); // shorter than the renewal margin
    const first = createCognitoSignIn(settings, fetchImpl);
    await first.signIn("alice", "Demo-pass-2026");
    const reloaded = createCognitoSignIn(settings, fetchImpl);
    expect(await reloaded.restore()).toEqual({ token: "access-2", subject: "alice" });
    expect(calls.at(-1)).toMatchObject({ flow: "REFRESH_TOKEN_AUTH", params: { REFRESH_TOKEN: "refresh-1" } });
    reloaded.signOut();
  });

  it("renews before the access token expires and tells the app", async () => {
    vi.useFakeTimers();
    const { fetchImpl } = fakeCognito({ expiresIn: 120 });
    const client = createCognitoSignIn(settings, fetchImpl);
    const renewed = vi.fn();
    client.onRenewed(renewed);
    await client.signIn("alice", "Demo-pass-2026");
    await vi.advanceTimersByTimeAsync(59_000);
    expect(renewed).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(2_000);
    expect(renewed).toHaveBeenCalledWith({ token: "access-2", subject: "alice" });
    client.signOut();
    expect(sessionStorage.getItem(COGNITO_SESSION_KEY)).toBeNull();
  });

  it("forgets a session it cannot renew", async () => {
    sessionStorage.setItem(COGNITO_SESSION_KEY, JSON.stringify({ username: "alice", accessToken: "old", refreshToken: "revoked", expiresAt: Date.now() - 1000 }));
    const { fetchImpl } = fakeCognito();
    expect(await createCognitoSignIn(settings, fetchImpl).restore()).toBeNull();
    expect(sessionStorage.getItem(COGNITO_SESSION_KEY)).toBeNull();
  });

  it("lists its storage key in COOKIES_AND_STORAGE.md", () => {
    expect(cookiesDoc).toContain(`\`${COGNITO_SESSION_KEY}\``);
  });
});

describe("Studio sign-in with Cognito", () => {
  it("shows a labelled username and password form instead of the demo users, and signs in", async () => {
    const { fetchImpl } = fakeCognito();
    const api = vi.fn(async (url: string) => (url.endsWith("/me") ? Response.json(alice) : Response.json([])));
    vi.stubGlobal("fetch", api);
    render(<App companySignIn={null} cognitoSignIn={createCognitoSignIn(settings, fetchImpl)} />);

    const form = await screen.findByRole("form", { name: "Sign in with Cognito" });
    expect(screen.queryByRole("button", { name: /Alice/ })).toBeNull();
    const username = screen.getByLabelText("Username") as HTMLInputElement;
    const password = screen.getByLabelText("Password") as HTMLInputElement;
    expect(username.autocomplete).toBe("username");
    expect(password.type).toBe("password");
    expect(password.autocomplete).toBe("current-password");

    // The terms come first, as with every sign-in.
    fireEvent.change(username, { target: { value: "alice" } });
    fireEvent.change(password, { target: { value: "wrong" } });
    fireEvent.submit(form);
    expect(screen.getByRole("alert").textContent).toContain("Tick the box");

    fireEvent.click(screen.getByRole("checkbox", { name: /accept the Terms of Use/ }));
    fireEvent.submit(form);
    expect((await screen.findByRole("alert")).textContent).toBe("Incorrect username or password.");
    expect(password.value).toBe("");

    fireEvent.change(password, { target: { value: "Demo-pass-2026" } });
    fireEvent.submit(form);
    await screen.findByText("Customer service", { selector: "option" });
    const [, init] = api.mock.calls.find(([url]) => String(url).endsWith("/me")) as unknown as [string, RequestInit];
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer access-1");
    // The access token is not copied into the development session store.
    expect(sessionStorage.getItem("bdg.session")).toBeNull();
  });

  it("asks for both fields before contacting Cognito", async () => {
    const { fetchImpl } = fakeCognito();
    render(<App companySignIn={null} cognitoSignIn={createCognitoSignIn(settings, fetchImpl)} />);
    fireEvent.click(await screen.findByRole("checkbox", { name: /accept the Terms of Use/ }));
    fireEvent.submit(screen.getByRole("form", { name: "Sign in with Cognito" }));
    expect(screen.getByRole("alert").textContent).toBe("Enter your username and password.");
    expect(document.activeElement).toBe(screen.getByLabelText("Username"));
    expect(fetchImpl).not.toHaveBeenCalled();
  });
});

describe("Company sign-in button", () => {
  it("names the provider, for Cognito managed login", async () => {
    const { Login } = await import("./Login");
    render(<Login onSignedIn={vi.fn()} startCompanySignIn={vi.fn(async () => {})} providerName="Example Bank" />);
    expect(screen.getByRole("button", { name: "Sign in with Example Bank" })).toBeTruthy();
  });
});
