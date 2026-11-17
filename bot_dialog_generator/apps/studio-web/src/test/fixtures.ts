import { vi } from "vitest";
import type { Bot, Me, NodeItem, StudioApi, Version } from "../api";

// A fake Studio API with the same two workspaces as the seeded studio-api store.
export const now = new Date().toISOString();

export const nodes: NodeItem[] = [
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

export const botsByWorkspace: Record<string, Bot[]> = {
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
export function fakeApi(me: Me, overrides: Partial<StudioApi> = {}) {
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
    updateBotSettings: vi.fn(async (_w: string, b: string, settings: object) => ({ ...botsByWorkspace["ws-customer-service"][0], id: b, ...settings })),
    acceptTerms: vi.fn(async () => undefined),
    listMyDeletionRequests: vi.fn(async () => []),
    requestMyDeletion: vi.fn(async (note: string) => ({ id: "staff-1", subject: "alice", note, status: "pending" as const, result: [], created_at: now })),
    listSubjectRequests: vi.fn(async () => []),
    eraseSubject: vi.fn(async (workspace_id: string, body: { subject_kind: "chat_user" | "email" }) => ({
      id: "req-1",
      workspace_id,
      subject_kind: body.subject_kind,
      subject_hash: "ab".repeat(32),
      source: "studio" as const,
      verification: "x",
      requested_by: "alice",
      status: "completed",
      result: [{ category: "consent_records", action: "deleted" as const, count: 2 }],
      created_at: now,
    })),
    adminListDeletionRequests: vi.fn(async () => []),
    adminExecuteDeletionRequest: vi.fn(async (id: string) => ({ id, subject: "erin", status: "completed" as const, result: [], created_at: now })),
    ...overrides,
  };
  return api as typeof api & StudioApi;
}

export const member = (subject: string, role: "owner" | "editor" | "analyst"): Me => ({
  subject,
  platform_admin: false,
  workspaces: [{ id: "ws-customer-service", name: "Customer service", role }],
});
export const alice = member("alice", "owner");
export const carol = member("carol", "analyst");
export const dana: Me = { subject: "dana", platform_admin: true, workspaces: [] };

