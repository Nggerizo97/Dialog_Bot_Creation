import { describe, it, expect, vi, beforeEach } from "vitest";

const manager = {
  settings: {} as Record<string, unknown>,
  signinRedirect: vi.fn(async () => {}),
  signinRedirectCallback: vi.fn(),
  getUser: vi.fn(),
  signinSilent: vi.fn(),
  removeUser: vi.fn(async () => {}),
  events: { addUserLoaded: vi.fn(), removeUserLoaded: vi.fn() },
};

vi.mock("oidc-client-ts", () => ({
  UserManager: vi.fn(function (this: unknown, settings: Record<string, unknown>) {
    manager.settings = settings;
    return manager;
  }),
  WebStorageStateStore: vi.fn(function (this: { options: unknown }, options: unknown) {
    this.options = options;
  }),
}));

import { createCompanySignIn, oidcSettings } from "./companySignIn";

const settings = {
  authority: "https://login.microsoftonline.com/00000000-0000-0000-0000-000000000000/v2.0",
  clientId: "11111111-2222-3333-4444-555555555555",
  scope: "openid profile offline_access api://11111111-2222-3333-4444-555555555555/access_as_user",
};
const entraUser = {
  access_token: "entra-access-token",
  expired: false,
  profile: { sub: "opaque", preferred_username: "ana@contoso.example" },
};

describe("oidcSettings", () => {
  it("is off unless both authority and client ID are set", () => {
    expect(oidcSettings({ VITE_OIDC_AUTHORITY: settings.authority } as ImportMetaEnv)).toBeNull();
    expect(oidcSettings({ VITE_OIDC_CLIENT_ID: settings.clientId } as ImportMetaEnv)).toBeNull();
    expect(oidcSettings({ VITE_OIDC_AUTHORITY: settings.authority, VITE_OIDC_CLIENT_ID: settings.clientId } as ImportMetaEnv)).toEqual({
      authority: settings.authority,
      clientId: settings.clientId,
      scope: "openid profile offline_access",
    });
  });
});

describe("createCompanySignIn", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    window.history.replaceState(null, "", "/");
  });

  it("uses the Authorization Code flow with PKCE and keeps the session for the tab only", () => {
    createCompanySignIn(settings);
    expect(manager.settings).toMatchObject({
      authority: settings.authority,
      client_id: settings.clientId,
      response_type: "code",
      scope: settings.scope,
      redirect_uri: `${window.location.origin}/`,
      automaticSilentRenew: true,
    });
    expect((manager.settings.userStore as { options: { store: Storage } }).options.store).toBe(window.sessionStorage);
  });

  it("does nothing on a normal page load", async () => {
    expect(await createCompanySignIn(settings).complete()).toBeNull();
    expect(manager.signinRedirectCallback).not.toHaveBeenCalled();
  });

  it("redeems the code once, even if asked twice, and removes it from the address bar", async () => {
    window.history.replaceState(null, "", "/?code=one-time-code&state=abc");
    manager.signinRedirectCallback.mockResolvedValue(entraUser);
    const signIn = createCompanySignIn(settings);
    const [first, second] = await Promise.all([signIn.complete(), signIn.complete()]);
    expect(first).toEqual({ token: "entra-access-token", subject: "ana@contoso.example" });
    expect(second).toBe(first);
    expect(manager.signinRedirectCallback).toHaveBeenCalledTimes(1);
    expect(window.location.search).toBe("");
  });

  it("renews an expired session, and signs out when renewal fails", async () => {
    const signIn = createCompanySignIn(settings);
    manager.getUser.mockResolvedValue({ ...entraUser, expired: true });
    manager.signinSilent.mockResolvedValue({ ...entraUser, access_token: "renewed" });
    expect((await signIn.restore())?.token).toBe("renewed");

    manager.signinSilent.mockRejectedValue(new Error("invalid_grant"));
    expect(await signIn.restore()).toBeNull();
    expect(manager.removeUser).toHaveBeenCalled();
  });
});
