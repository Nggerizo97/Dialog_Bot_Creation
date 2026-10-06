// Client for the Studio API (contracts/openapi/studio-api.yaml).

export const API_BASE: string = import.meta.env.VITE_STUDIO_API_URL ?? "http://localhost:8080";

export type Role = "analyst" | "editor" | "owner";

export interface Workspace {
  id: string;
  name: string;
  archived_at?: string;
}

/** A person with a direct role in a workspace. */
export interface Member {
  subject: string;
  role: Role;
}

/** An identity-provider group with a role in a workspace. With Entra ID, group_id is the group's object ID. */
export interface GroupGrant {
  group_id: string;
  display_name: string;
  role: Role;
}

export interface Access {
  members: Member[];
  groups: GroupGrant[];
}

export interface WorkspaceAccess extends Workspace {
  role: Role;
}

export interface Me {
  subject: string;
  email?: string;
  platform_admin: boolean;
  workspaces: WorkspaceAccess[];
}

export interface Bot {
  id: string;
  workspace_id: string;
  name: string;
  description?: string;
  active_version?: string;
  draft_version: string;
  updated_at: string;
}

export interface NodeItem {
  id: string;
  type: string;
  title: string;
  detail?: string;
  properties?: Record<string, string>;
}

export interface EdgeItem {
  from: string;
  to: string;
  condition?: string;
}

export interface Version {
  id: string;
  bot_id: string;
  version: string;
  status: "draft" | "published" | "archived";
  entry_node_id: string;
  nodes: NodeItem[] | null;
  edges: EdgeItem[] | null;
  updated_at: string;
}

export interface DraftContent {
  entry_node_id: string;
  nodes: NodeItem[];
  edges: EdgeItem[];
}

export interface PublishResult {
  bot_id: string;
  version: string;
  status: string;
  sha256: string;
  artifact_uri: string;
}

export interface AuditEntry {
  at: string;
  subject: string;
  workspace_id?: string;
  action: string;
  resource: string;
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

export interface StudioApi {
  me(): Promise<Me>;
  listBots(workspaceId: string): Promise<Bot[]>;
  createBot(workspaceId: string, name: string): Promise<Bot>;
  getVersion(workspaceId: string, botId: string, version: string): Promise<Version>;
  listVersions(workspaceId: string, botId: string): Promise<Version[]>;
  saveDraft(workspaceId: string, botId: string, version: string, draft: DraftContent): Promise<Version>;
  createDraft(workspaceId: string, botId: string, baseVersion: string): Promise<Version>;
  publish(workspaceId: string, botId: string, version: string): Promise<PublishResult>;
  setActiveVersion(workspaceId: string, botId: string, version: string): Promise<unknown>;
  listAccess(workspaceId: string): Promise<Access>;
  setMember(workspaceId: string, subject: string, role: Role): Promise<Member>;
  removeMember(workspaceId: string, subject: string): Promise<void>;
  setGroupGrant(workspaceId: string, grant: GroupGrant): Promise<GroupGrant>;
  removeGroupGrant(workspaceId: string, groupId: string): Promise<void>;
  adminCreateWorkspace(name: string, access: Access): Promise<Workspace>;
  adminUpdateWorkspace(workspaceId: string, change: { name?: string; archived?: boolean }): Promise<Workspace>;
  adminListWorkspaces(): Promise<Workspace[]>;
  adminListBots(): Promise<Bot[]>;
  adminListAudit(): Promise<AuditEntry[]>;
}

export function createApi(token: string, base: string = API_BASE): StudioApi {
  async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { Authorization: `Bearer ${token}` };
    if (body !== undefined) headers["Content-Type"] = "application/json";
    const res = await fetch(`${base}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (!res.ok) {
      let detail = res.statusText;
      try {
        const problem = await res.json();
        detail = problem.detail ?? detail;
      } catch {
        // Not a Problem Details body; keep the status text.
      }
      throw new ApiError(res.status, detail);
    }
    if (res.status === 204) return undefined as T;
    return res.json() as Promise<T>;
  }

  const ws = (id: string) => `/workspaces/${encodeURIComponent(id)}`;
  const bot = (workspaceId: string, botId: string) => `${ws(workspaceId)}/bots/${encodeURIComponent(botId)}`;
  const ver = (workspaceId: string, botId: string, version: string) =>
    `${bot(workspaceId, botId)}/versions/${encodeURIComponent(version)}`;

  return {
    me: () => request("GET", "/me"),
    listBots: (w) => request("GET", `${ws(w)}/bots`),
    createBot: (w, name) => request("POST", `${ws(w)}/bots`, { name }),
    getVersion: (w, b, v) => request("GET", ver(w, b, v)),
    listVersions: (w, b) => request("GET", `${bot(w, b)}/versions`),
    saveDraft: (w, b, v, draft) => request("PUT", ver(w, b, v), draft),
    createDraft: (w, b, baseVersion) => request("POST", `${bot(w, b)}/versions`, { base_version: baseVersion }),
    publish: (w, b, v) => request("POST", `${ver(w, b, v)}/publish`),
    setActiveVersion: (w, b, v) => request("PUT", `${bot(w, b)}/active-version`, { version: v }),
    listAccess: (w) => request("GET", `${ws(w)}/members`),
    setMember: (w, subject, role) => request("PUT", `${ws(w)}/members/${encodeURIComponent(subject)}`, { role }),
    removeMember: (w, subject) => request("DELETE", `${ws(w)}/members/${encodeURIComponent(subject)}`),
    setGroupGrant: (w, g) =>
      request("PUT", `${ws(w)}/groups/${encodeURIComponent(g.group_id)}`, { display_name: g.display_name, role: g.role }),
    removeGroupGrant: (w, groupId) => request("DELETE", `${ws(w)}/groups/${encodeURIComponent(groupId)}`),
    adminCreateWorkspace: (name, access) => request("POST", "/admin/workspaces", { name, ...access }),
    adminUpdateWorkspace: (w, change) => request("PATCH", `/admin/workspaces/${encodeURIComponent(w)}`, change),
    adminListWorkspaces: () => request("GET", "/admin/workspaces"),
    adminListBots: () => request("GET", "/admin/bots"),
    adminListAudit: () => request("GET", "/admin/audit"),
  };
}

/** Development sign-in: asks studio-api (AUTH_MODE=dev only) to mint a token. */
export async function devSignIn(subject: string, groups: string[], base: string = API_BASE): Promise<string> {
  let res: Response;
  try {
    res = await fetch(`${base}/dev/token`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ subject, groups }),
    });
  } catch {
    throw new Error(`Can't reach the Studio API at ${base}. Start it with: go run ./services/studio-api`);
  }
  if (!res.ok) {
    throw new ApiError(res.status, "Development sign-in is off. Run studio-api with AUTH_MODE=dev in a local environment.");
  }
  return (await res.json()).access_token;
}

export interface Session {
  token: string;
  subject: string;
}

// Development sessions live in sessionStorage: they survive a reload and end when the
// tab closes. Production sign-in will use the company identity provider instead.
const SESSION_KEY = "bdg.session";

export function loadSession(): Session | null {
  try {
    const raw = sessionStorage.getItem(SESSION_KEY);
    return raw ? (JSON.parse(raw) as Session) : null;
  } catch {
    return null;
  }
}

export function saveSession(session: Session | null): void {
  try {
    if (session) sessionStorage.setItem(SESSION_KEY, JSON.stringify(session));
    else sessionStorage.removeItem(SESSION_KEY);
  } catch {
    // Storage unavailable (private mode); the session lasts until reload.
  }
}
