import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, onTestFinished } from "vitest";
import { App, Studio } from "./App";
import type { CompanySignIn } from "./companySignIn";
import { Login, DEMO_USERS } from "./Login";
import { ApiError, type StudioApi } from "./api";
import { alice, carol, dana, fakeApi, now } from "./test/fixtures";

const acceptTerms = () => fireEvent.click(screen.getByRole("checkbox", { name: /accept the Terms of Use/ }));

const renderStudio = async (api: StudioApi) => {
  const onSignOut = vi.fn();
  render(<Studio api={api} onSignOut={onSignOut} />);
  return { onSignOut };
};

describe("Sign-in", () => {
  beforeEach(() => sessionStorage.clear());

  it("shows the sign-in screen when there is no session", () => {
    render(<App />);
    expect(screen.getByRole("heading", { name: "Sign in" })).toBeDefined();
    for (const user of DEMO_USERS) {
      expect(screen.getByText(user.label)).toBeDefined();
    }
  });

  it("signs in as the chosen demo user, including group-based access", async () => {
    const onSignedIn = vi.fn();
    const signIn = vi.fn(async (subject: string) => `token-${subject}`);
    render(<Login onSignedIn={onSignedIn} signIn={signIn} />);
    acceptTerms();
    fireEvent.click(screen.getByText("Frank"));
    await waitFor(() => expect(onSignedIn).toHaveBeenCalledWith({ token: "token-frank", subject: "frank" }));
    expect(signIn).toHaveBeenCalledWith("frank", ["hr-team"]);
  });

  it("explains why sign-in failed", async () => {
    const signIn = vi.fn(async () => {
      throw new Error("Can't reach the Studio API at http://localhost:8080.");
    });
    render(<Login onSignedIn={vi.fn()} signIn={signIn} />);
    acceptTerms();
    fireEvent.click(screen.getByText("Alice"));
    expect((await screen.findByRole("alert")).textContent).toContain("Can't reach the Studio API");
  });
});

describe("Studio", () => {
  it("loads only the member's workspace and its bots", async () => {
    const api = fakeApi(alice);
    await renderStudio(api);
    expect(await screen.findByText("Check balance")).toBeDefined();
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Retail assistant");
    expect((screen.getByLabelText("Workspace") as HTMLSelectElement).value).toBe("ws-customer-service");
    expect(api.listBots).toHaveBeenCalledTimes(1);
    expect(api.listBots).toHaveBeenCalledWith("ws-customer-service");
    expect(api.getVersion).toHaveBeenCalledWith("ws-customer-service", "retail-assistant", "v18");
  });

  it("starts with the first node selected and follows node selection", async () => {
    await renderStudio(fakeApi(alice));
    await screen.findByText("Check balance");
    const inspectorHeading = screen.getByRole("heading", { level: 2 });
    expect(inspectorHeading.textContent).toBe("Trigger");

    fireEvent.click(screen.getByText("Check balance"));
    expect(inspectorHeading.textContent).toBe("Service");

    const handoff = screen.getAllByText("Connect to advisor").find((el) => el.closest("button.flow-node"));
    fireEvent.click(handoff!);
    expect(inspectorHeading.textContent).toBe("Response");
  });

  it("lets owners publish, then opens a new draft", async () => {
    const api = fakeApi(alice);
    await renderStudio(api);
    await screen.findByText("Check balance");
    const publish = screen.getByRole("button", { name: "Publish bot" }) as HTMLButtonElement;
    expect(publish.disabled).toBe(false);
    fireEvent.click(publish);
    await waitFor(() =>
      expect(api.publish).toHaveBeenCalledWith("ws-customer-service", "retail-assistant", "v18", { parentalConsentConfirmed: false }),
    );
    await waitFor(() => expect(api.createDraft).toHaveBeenCalledWith("ws-customer-service", "retail-assistant", "v18"));
  });

  it("gives analysts a read-only designer", async () => {
    await renderStudio(fakeApi(carol));
    await screen.findByText("Check balance");
    expect((screen.getByRole("button", { name: "Publish bot" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole("button", { name: "Save draft" }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByLabelText("Node name").closest("fieldset")?.disabled).toBe(true);
    expect(screen.queryByRole("button", { name: "Add node" })).toBeNull();
    expect(screen.getByText("Your role in this workspace is read-only.")).toBeDefined();
  });

  it("keeps the flow's edges when saving and drops edges to deleted nodes", async () => {
    const api = fakeApi(alice);
    await renderStudio(api);
    fireEvent.click(await screen.findByText("Check balance"));
    fireEvent.click(screen.getByText("Delete this node"));
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(api.saveDraft).toHaveBeenCalled());
    const [, , savedVersion, draft] = vi.mocked(api.saveDraft).mock.calls[0];
    expect(savedVersion).toBe("v18");
    expect(draft).toMatchObject({
      entry_node_id: "welcome",
      edges: [
        { from: "welcome", to: "menu" },
        { from: "menu", to: "handoff", condition: "handoff" },
      ],
    });
  });

  it("takes a platform admin with no memberships to the admin page", async () => {
    await renderStudio(fakeApi(dana));
    expect(await screen.findByText("Platform administration")).toBeDefined();
    expect(await screen.findByText("All bots (2)")).toBeDefined();
    expect(screen.getByText("HR helpdesk")).toBeDefined();
    expect(screen.getByText("admin.read GET /admin/bots")).toBeDefined();
  });

  it("signs out when the session has expired", async () => {
    const api = fakeApi(alice, {
      me: vi.fn(async () => {
        throw new ApiError(401, "The bearer token is invalid or expired.");
      }),
    });
    const { onSignOut } = await renderStudio(api);
    await waitFor(() => expect(onSignOut).toHaveBeenCalled());
  });
  it("asks whether to use the AI assistant or the contact center before handing off", async () => {
    await renderStudio(fakeApi(alice));
    await screen.findByText("Check balance");
    fireEvent.click(screen.getByRole("button", { name: "Test" }));
    const chatButton = async (label: string) =>
      (await screen.findAllByRole("button", { name: label })).find((el) => el.classList.contains("test-menu-btn"))!;

    fireEvent.click(await chatButton("Connect to advisor"));
    await screen.findByText("Would you like to chat with our AI assistant or talk to a person from the contact center?");

    fireEvent.click(await chatButton("AI assistant"));
    await screen.findByText("You can talk to a person at any time.");

    fireEvent.click(await chatButton("Talk to a person"));
    await screen.findByText(/no agent will join/);
  });
});

describe("Members and areas", () => {
  const groupId = "1b2c3d4e-5f60-4718-8a9b-0c1d2e3f4a5b";

  it("lets owners add Entra groups and people to their area", async () => {
    const api = fakeApi(alice);
    await renderStudio(api);
    fireEvent.click(await screen.findByText("Members"));
    await screen.findByText("CS agents");

    fireEvent.change(screen.getByLabelText("Group object ID"), { target: { value: ` ${groupId} ` } });
    fireEvent.change(screen.getByLabelText("Display name"), { target: { value: "Legal editors" } });
    fireEvent.change(screen.getByLabelText("Role for the new group"), { target: { value: "analyst" } });
    fireEvent.click(screen.getByRole("button", { name: "Add group" }));
    await waitFor(() =>
      expect(api.setGroupGrant).toHaveBeenCalledWith("ws-customer-service", { group_id: groupId, display_name: "Legal editors", role: "analyst" }),
    );

    fireEvent.change(screen.getByLabelText("Person (user ID)"), { target: { value: "luis" } });
    fireEvent.click(screen.getByRole("button", { name: "Add person" }));
    await waitFor(() => expect(api.setMember).toHaveBeenCalledWith("ws-customer-service", "luis", "editor"));

    fireEvent.change(screen.getByLabelText("Role for carol"), { target: { value: "editor" } });
    await waitFor(() => expect(api.setMember).toHaveBeenCalledWith("ws-customer-service", "carol", "editor"));
  });

  it("shows analysts who has access without the controls to change it", async () => {
    await renderStudio(fakeApi(carol));
    fireEvent.click(await screen.findByText("Members"));
    await screen.findByText("CS agents");
    expect(screen.queryByRole("button", { name: "Add group" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Remove" })).toBeNull();
    expect(screen.queryByLabelText("Role for carol")).toBeNull();
    expect(screen.getByText("Ask an owner to change who has access.", { exact: false })).toBeTruthy();
  });

  it("lets platform admins create, archive and restore areas", async () => {
    const api = fakeApi(dana, {
      adminListWorkspaces: vi.fn(async () => [
        { id: "ws-customer-service", name: "Customer service" },
        { id: "ws-hr", name: "Human resources", archived_at: now },
      ]),
    });
    vi.stubGlobal("confirm", vi.fn(() => true));
    onTestFinished(() => {
      vi.unstubAllGlobals();
    });
    await renderStudio(api);
    await screen.findByText("Archived");

    fireEvent.change(screen.getByLabelText("New area"), { target: { value: "Legal" } });
    fireEvent.change(screen.getByLabelText("Owner (user ID)"), { target: { value: "ana" } });
    fireEvent.change(screen.getByLabelText("Entra group object ID"), { target: { value: groupId } });
    fireEvent.change(screen.getByLabelText("Group name"), { target: { value: "Legal editors" } });
    fireEvent.click(screen.getByRole("button", { name: "Create area" }));
    await waitFor(() =>
      expect(api.adminCreateWorkspace).toHaveBeenCalledWith("Legal", {
        members: [{ subject: "ana", role: "owner" }],
        groups: [{ group_id: groupId, display_name: "Legal editors", role: "editor" }],
      }),
    );

    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    await waitFor(() => expect(api.adminUpdateWorkspace).toHaveBeenCalledWith("ws-customer-service", { archived: true }));
    fireEvent.click(screen.getByRole("button", { name: "Restore" }));
    await waitFor(() => expect(api.adminUpdateWorkspace).toHaveBeenCalledWith("ws-hr", { archived: false }));
  });
  it("keeps the last owner from being removed or demoted", async () => {
    // alice is the only owner in the fake access list.
    await renderStudio(fakeApi(alice));
    fireEvent.click(await screen.findByText("Members"));
    await screen.findByText("CS agents");
    expect(screen.getByRole("note").textContent).toContain("can't be removed or changed until you make someone else an owner");

    const rowOf = (name: string) => screen.getByText(name, { selector: "strong" }).closest("tr")!;
    expect((screen.getByLabelText("Role for alice") as HTMLSelectElement).disabled).toBe(true);
    expect((rowOf("alice").querySelector("button") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByLabelText("Role for carol") as HTMLSelectElement).disabled).toBe(false);
    expect((rowOf("carol").querySelector("button") as HTMLButtonElement).disabled).toBe(false);
  });
});

describe("Accessibility", () => {
  it("makes the sidebar keyboard reachable and marks the current page", async () => {
    await renderStudio(fakeApi(alice));
    const designer = await screen.findByRole("button", { name: "Designer" });
    const members = screen.getByRole("button", { name: "Members" });
    expect(designer.getAttribute("aria-current")).toBe("page");
    expect(members.getAttribute("aria-current")).toBeNull();
    fireEvent.click(members);
    await waitFor(() => expect(screen.getByRole("button", { name: "Members" }).getAttribute("aria-current")).toBe("page"));
  });

  it("labels the test chat's close button and announces its messages", async () => {
    await renderStudio(fakeApi(alice));
    await screen.findByText("Check balance");
    fireEvent.click(screen.getByRole("button", { name: "Test" }));
    expect(await screen.findByRole("button", { name: "Close test chat" })).toBeTruthy();
    expect(screen.getByRole("log", { name: "Test chat messages" }).getAttribute("aria-live")).toBe("polite");
  });
});

describe("Company sign-in (Entra ID)", () => {
  const fakeCompanySignIn = (overrides: Partial<CompanySignIn> = {}): CompanySignIn => ({
    start: vi.fn(async () => {}),
    complete: vi.fn(async () => null),
    restore: vi.fn(async () => null),
    signOut: vi.fn(async () => {}),
    onRenewed: vi.fn(() => () => {}),
    ...overrides,
  });

  it("offers Microsoft sign-in instead of the demo users when configured", async () => {
    const company = fakeCompanySignIn();
    render(<App companySignIn={company} />);
    const button = await screen.findByRole("button", { name: "Sign in with Microsoft" });
    expect(screen.queryByRole("button", { name: /Alice/ })).toBeNull();
    acceptTerms();
    fireEvent.click(button);
    await waitFor(() => expect(company.start).toHaveBeenCalled());
  });

  it("finishes the redirect and calls the API with the Entra access token", async () => {
    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify({ subject: "a0b1", platform_admin: false, workspaces: [] }), {
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    onTestFinished(() => {
      vi.unstubAllGlobals();
    });
    const company = fakeCompanySignIn({ complete: vi.fn(async () => ({ token: "entra-access-token", subject: "ana@contoso.example" })) });
    render(<App companySignIn={company} />);
    await screen.findByText("No workspace yet");
    const [, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer entra-access-token");
    expect(company.restore).not.toHaveBeenCalled();
  });

  it("shows why sign-in failed, for example when consent is refused", async () => {
    const company = fakeCompanySignIn({
      complete: vi.fn(async () => {
        throw new Error("AADSTS65004: User declined to consent to access the app.");
      }),
    });
    render(<App companySignIn={company} />);
    expect((await screen.findByRole("alert")).textContent).toContain("declined to consent");
    expect(screen.getByRole("button", { name: "Sign in with Microsoft" })).toBeTruthy();
  });
});
