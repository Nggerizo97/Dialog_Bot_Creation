import { useEffect, useState } from "react";
import { ShieldCheck } from "lucide-react";
import type { AuditEntry, Bot, StudioApi, Workspace } from "./api";

export function AdminView({ api, onError }: { api: StudioApi; onError: (e: unknown) => void }) {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [bots, setBots] = useState<Bot[]>([]);
  const [audit, setAudit] = useState<AuditEntry[]>([]);

  useEffect(() => {
    Promise.all([api.adminListWorkspaces(), api.adminListBots(), api.adminListAudit()])
      .then(([w, b, a]) => {
        setWorkspaces(w);
        setBots(b);
        setAudit([...a].reverse());
      })
      .catch(onError);
  }, [api]);

  const workspaceName = (id?: string) => workspaces.find((w) => w.id === id)?.name ?? id ?? "—";

  return (
    <section className="workspace versions-view admin-view">
      <h2>
        <ShieldCheck size={18} /> Platform administration
      </h2>
      <p className="admin-note">
        You can see every workspace. Opening this page is recorded in the audit log below.
      </p>

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
