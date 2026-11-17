import { useEffect, useState } from "react";
import { ShieldCheck } from "lucide-react";
import type { Access, AuditEntry, Bot, Role, StaffRequest, StudioApi, Workspace } from "./api";
import { RoleSelect } from "./MembersView";
import { ErasureResult } from "./PrivacyView";

export function AdminView({
  api,
  onError,
  onStatus = () => {},
}: {
  api: StudioApi;
  onError: (e: unknown) => void;
  onStatus?: (msg: string) => void;
}) {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [bots, setBots] = useState<Bot[]>([]);
  const [audit, setAudit] = useState<AuditEntry[]>([]);
  const [deletions, setDeletions] = useState<StaffRequest[]>([]);
  const [areaName, setAreaName] = useState("");
  const [owner, setOwner] = useState("");
  const [groupId, setGroupId] = useState("");
  const [groupName, setGroupName] = useState("");
  const [groupRole, setGroupRole] = useState<Role>("editor");

  const refresh = () =>
    Promise.all([api.adminListWorkspaces(), api.adminListBots(), api.adminListAudit(), api.adminListDeletionRequests()])
      .then(([w, b, a, d]) => {
        setWorkspaces(w);
        setBots(b);
        setAudit([...a].reverse());
        setDeletions(d);
      })
      .catch(onError);

  useEffect(() => {
    refresh();
  }, [api]);

  const workspaceName = (id?: string) => workspaces.find((w) => w.id === id)?.name ?? id ?? "—";

  const run = async (action: () => Promise<unknown>, message: string) => {
    try {
      await action();
      onStatus(message);
      await refresh();
      return true;
    } catch (e) {
      onError(e);
      return false;
    }
  };

  const createArea = async () => {
    const name = areaName.trim();
    const access: Access = { members: [], groups: [] };
    if (owner.trim()) access.members.push({ subject: owner.trim(), role: "owner" });
    if (groupId.trim()) access.groups.push({ group_id: groupId.trim(), display_name: groupName.trim(), role: groupRole });
    if (await run(() => api.adminCreateWorkspace(name, access), `Created ${name}`)) {
      setAreaName("");
      setOwner("");
      setGroupId("");
      setGroupName("");
    }
  };

  const rename = (ws: Workspace) => {
    const name = window.prompt(`New name for ${ws.name}`, ws.name)?.trim();
    if (name && name !== ws.name) run(() => api.adminUpdateWorkspace(ws.id, { name }), `Renamed to ${name}`);
  };

  return (
    <section className="workspace versions-view admin-view">
      <h2>
        <ShieldCheck size={18} aria-hidden="true" /> Platform administration
      </h2>
      <p className="admin-note">
        You can see every workspace. Opening this page is recorded in the audit log below.
      </p>

      <h3>Areas ({workspaces.length})</h3>
      <table className="versions-table">
        <thead>
          <tr>
            <th>Area</th>
            <th>Bots</th>
            <th>Status</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {workspaces.map((ws) => (
            <tr key={ws.id}>
              <td>
                <strong>{ws.name}</strong>
              </td>
              <td>{bots.filter((b) => b.workspace_id === ws.id).length}</td>
              <td>{ws.archived_at ? "Archived" : "Active"}</td>
              <td>
                <button className="action-btn" onClick={() => rename(ws)}>
                  Rename
                </button>{" "}
                {ws.archived_at ? (
                  <button className="action-btn" onClick={() => run(() => api.adminUpdateWorkspace(ws.id, { archived: false }), `Restored ${ws.name}`)}>
                    Restore
                  </button>
                ) : (
                  <button
                    className="action-btn"
                    onClick={() => {
                      if (window.confirm(`Archive ${ws.name}? Its members lose access until you restore it.`)) {
                        run(() => api.adminUpdateWorkspace(ws.id, { archived: true }), `Archived ${ws.name}`);
                      }
                    }}
                  >
                    Archive
                  </button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <form
        className="inline-form"
        aria-label="Create an area"
        onSubmit={(e) => {
          e.preventDefault();
          createArea();
        }}
      >
        <label>
          New area
          <input value={areaName} onChange={(e) => setAreaName(e.target.value)} placeholder="Legal" required />
        </label>
        <label>
          Owner (user ID)
          <input value={owner} onChange={(e) => setOwner(e.target.value)} placeholder="ana" />
        </label>
        <label>
          Entra group object ID
          <input value={groupId} onChange={(e) => setGroupId(e.target.value)} placeholder="optional" />
        </label>
        <label>
          Group name
          <input value={groupName} onChange={(e) => setGroupName(e.target.value)} placeholder="Legal editors" />
        </label>
        <label>
          Group role
          <RoleSelect label="Role for the area's group" value={groupRole} onChange={setGroupRole} />
        </label>
        <button className="test-button" type="submit">
          Create area
        </button>
      </form>
      <p className="admin-note">An area needs an owner: a person, or a group with the owner role.</p>

      <h3>All bots ({bots.length})</h3>
      <table className="versions-table">
        <thead>
          <tr>
            <th>Bot</th>
            <th>Workspace</th>
            <th>Active version</th>
            <th>Draft</th>
          </tr>
        </thead>
        <tbody>
          {bots.map((bot) => (
            <tr key={bot.id}>
              <td>
                <strong>{bot.name}</strong>
              </td>
              <td>{workspaceName(bot.workspace_id)}</td>
              <td>{bot.active_version ?? "Not published"}</td>
              <td>{bot.draft_version}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <h3>Staff deletion requests ({deletions.filter((d) => d.status !== "completed").length} open)</h3>
      <p className="admin-note">
        Running a request removes the person's access to every area, except where they are the only owner. Audit entries and the bots they
        built are kept.
      </p>
      <table className="versions-table">
        <thead>
          <tr>
            <th scope="col">Requested</th>
            <th scope="col">Person</th>
            <th scope="col">Note</th>
            <th scope="col">Status</th>
            <th scope="col" />
          </tr>
        </thead>
        <tbody>
          {deletions.length === 0 ? (
            <tr>
              <td colSpan={5}>No requests.</td>
            </tr>
          ) : (
            deletions.map((d) => (
              <tr key={d.id}>
                <td>{new Date(d.created_at).toLocaleString()}</td>
                <td>{d.subject}</td>
                <td>{d.note || "—"}</td>
                <td>
                  {d.status === "pending" ? "Pending" : d.status === "completed" ? "Completed" : "Partly completed"}
                  <ErasureResult result={d.result} />
                </td>
                <td>
                  {d.status !== "completed" && (
                    <button
                      className="action-btn"
                      onClick={() => {
                        if (window.confirm(`Remove ${d.subject}'s access to every area? This cannot be undone.`)) {
                          run(() => api.adminExecuteDeletionRequest(d.id), `Ran the deletion request for ${d.subject}`);
                        }
                      }}
                    >
                      Run request
                    </button>
                  )}
                </td>
              </tr>
            ))
          )}
        </tbody>
      </table>

      <h3>Audit log</h3>
      <table className="versions-table">
        <thead>
          <tr>
            <th>When</th>
            <th>Who</th>
            <th>Workspace</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          {audit.slice(0, 50).map((entry, i) => (
            <tr key={`${entry.at}-${i}`}>
              <td>{new Date(entry.at).toLocaleString()}</td>
              <td>{entry.subject}</td>
              <td>{workspaceName(entry.workspace_id)}</td>
              <td>
                <code>{entry.action}</code>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}
