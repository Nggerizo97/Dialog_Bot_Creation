import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, onTestFinished } from "vitest";
import { App, Studio } from "./App";
import { Login, DEMO_USERS } from "./Login";
import { ApiError, type Bot, type Me, type NodeItem, type StudioApi, type Version } from "./api";

const now = new Date().toISOString();

const nodes: NodeItem[] = [
  { id: "welcome", type: "Trigger", title: "Welcome", detail: "New conversation" },
  { id: "menu", type: "Menu", title: "What can we help with?", detail: "3 routes" },
  { id: "balance", type: "Service", title: "Check balance", detail: "Accounts API" },
  { id: "handoff", type: "Response", title: "Connect to advisor", detail: "Text message" },
];
const edges = [
  { from: "welcome", to: "menu" },
  { from: "menu", to: "balance", condition: "balance" },
  { from: "menu", to: "handoff", condition: "handoff" },
];

const botsByWorkspace: Record<string, Bot[]> = {
  "ws-customer-service": [
    { id: "retail-assistant", workspace_id: "ws-customer-service", name: "Retail assistant", draft_version: "v18", active_version: "v17", updated_at: now },
  ],
  "ws-hr": [{ id: "hr-helpdesk", workspace_id: "ws-hr", name: "HR helpdesk", draft_version: "v1", updated_at: now }],
};

const version = (v: string, status: Version["status"] = "draft"): Version => ({
  id: v,
  bot_id: "retail-assistant",
  version: v,
  status,
  entry_node_id: "welcome",
  nodes,
  edges,
  updated_at: now,
});

// A fake Studio API with the same two workspaces as the seeded studio-api store.
function fakeApi(me: Me, overrides: Partial<StudioApi> = {}) {
  const api = {
    me: vi.fn(async () => me),
    listBots: vi.fn(async (ws: string) => botsByWorkspace[ws] ?? []),
    createBot: vi.fn(),
    getVersion: vi.fn(async (_w: string, _b: string, v: string) => version(v)),
    listVersions: vi.fn(async () => [version("v17", "published"), version("v18")]),
    saveDraft: vi.fn(async (_w: string, _b: string, v: string, draft: object) => ({ ...version(v), ...draft })),
    createDraft: vi.fn(async () => version("v19")),
    publish: vi.fn(async (_w: string, b: string, v: string) => ({ bot_id: b, version: v, status: "published", sha256: "abc", artifact_uri: "s3://x" })),
    setActiveVersion: vi.fn(async () => ({})),
    adminListWorkspaces: vi.fn(async () => [
      { id: "ws-customer-service", name: "Customer service" },
      { id: "ws-hr", name: "Human resources" },
    ]),
    adminListBots: vi.fn(async () => [...botsByWorkspace["ws-customer-service"], ...botsByWorkspace["ws-hr"]]),
    adminListAudit: vi.fn(async () => [{ at: now, subject: "dana", action: "admin.read GET /admin/bots", resource: "/admin/bots" }]),
    listAccess: vi.fn(async () => ({
      members: [
        { subject: "alice", role: "owner" as const },
        { subject: "carol", role: "analyst" as const },
      ],
      groups: [{ group_id: "6f1c2a9e-0000-0000-0000-000000000001", display_name: "CS agents", role: "editor" as const }],
    })),
    setMember: vi.fn(async (_w: string, subject: string, role: string) => ({ subject, role })),
    removeMember: vi.fn(async () => undefined),
    setGroupGrant: vi.fn(async (_w: string, g: object) => g),
    removeGroupGrant: vi.fn(async () => undefined),
    adminCreateWorkspace: vi.fn(async (name: string) => ({ id: "ws-legal", name })),
    adminUpdateWorkspace: vi.fn(async (id: string) => ({ id, name: "x" })),
    ...overrides,
  };
  return api as typeof api & StudioApi;
}

const member = (subject: string, role: "owner" | "editor" | "analyst"): Me => ({
  subject,
  platform_admin: false,
  workspaces: [{ id: "ws-customer-service", name: "Customer service", role }],
});
const alice = member("alice", "owner");
const carol = member("carol", "analyst");
const dana: Me = { subject: "dana", platform_admin: true, workspaces: [] };

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
    fireEvent.click(screen.getByText("Frank"));
    await waitFor(() => expect(onSignedIn).toHaveBeenCalledWith({ token: "token-frank", subject: "frank" }));
    expect(signIn).toHaveBeenCalledWith("frank", ["hr-team"]);
  });

  it("explains why sign-in failed", async () => {
    const signIn = vi.fn(async () => {
      throw new Error("Can't reach the Studio API at http://localhost:8080.");
    });
    render(<Login onSignedIn={vi.fn()} signIn={signIn} />);
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
    await waitFor(() => expect(api.publish).toHaveBeenCalledWith("ws-customer-service", "retail-assistant", "v18"));
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
    await screen.findByText("Connecting you with an available agent from the contact center now...");
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
});
