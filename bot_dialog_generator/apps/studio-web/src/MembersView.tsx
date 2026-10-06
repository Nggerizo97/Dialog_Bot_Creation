import { useEffect, useState } from "react";
import { Users } from "lucide-react";
import type { Access, Role, StudioApi, WorkspaceAccess } from "./api";

export const ROLES: Role[] = ["owner", "editor", "analyst"];

const ROLE_HELP: Record<Role, string> = {
  owner: "publishes bots and manages members",
  editor: "builds and edits bots",
  analyst: "reads bots and results",
};

const LAST_OWNER_HELP = "This is the area's last owner. Make someone else an owner first.";

export function RoleSelect({
  value,
  onChange,
  label,
  disabled = false,
}: {
  value: Role;
  onChange: (r: Role) => void;
  label: string;
  disabled?: boolean;
}) {
  return (
    <select
      aria-label={label}
      value={value}
      disabled={disabled}
      title={disabled ? LAST_OWNER_HELP : undefined}
      onChange={(e) => onChange(e.target.value as Role)}
    >
      {ROLES.map((r) => (
        <option key={r} value={r}>
          {r}
        </option>
      ))}
    </select>
  );
}

/**
 * People and Entra ID groups with access to one workspace. Everyone in the workspace
 * can see the list; only owners can change it.
 */
export function MembersView({
  api,
  workspace,
  onError,
  onStatus,
}: {
  api: StudioApi;
  workspace: WorkspaceAccess;
  onError: (e: unknown) => void;
  onStatus: (msg: string) => void;
}) {
  const [access, setAccess] = useState<Access>({ members: [], groups: [] });
  const [subject, setSubject] = useState("");
  const [memberRole, setMemberRole] = useState<Role>("editor");
  const [groupId, setGroupId] = useState("");
  const [groupName, setGroupName] = useState("");
  const [groupRole, setGroupRole] = useState<Role>("editor");
  const canManage = workspace.role === "owner";
  // studio-api refuses to remove or demote the last owner; say so before anyone tries.
  const owners = [...access.members, ...access.groups].filter((a) => a.role === "owner").length;
  const isLastOwner = (role: Role) => role === "owner" && owners === 1;

  const refresh = () => api.listAccess(workspace.id).then(setAccess).catch(onError);
  useEffect(() => {
    refresh();
  }, [api, workspace.id]);

  // run applies a change, then reloads the list. It reports whether the change worked.
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

  const addGroup = async () => {
    const id = groupId.trim();
    if (!id) return;
    const name = groupName.trim();
    if (await run(() => api.setGroupGrant(workspace.id, { group_id: id, display_name: name, role: groupRole }), `Added ${name || id}`)) {
      setGroupId("");
      setGroupName("");
    }
  };

  const addPerson = async () => {
    const who = subject.trim();
    if (!who) return;
    if (await run(() => api.setMember(workspace.id, who, memberRole), `Added ${who}`)) setSubject("");
  };

  return (
    <section className="workspace versions-view admin-view">
      <h2>
        <Users size={18} /> Members of {workspace.name}
      </h2>
      <p className="admin-note">
        Only the groups and people below can see this area's bots.{" "}
        {canManage ? "As an owner, you can change who has access." : "Ask an owner to change who has access."}
      </p>
      {canManage && owners === 1 && (
        <p className="admin-note last-owner-note" role="note">
          This area has one owner, who can't be removed or changed until you make someone else an owner.
        </p>
      )}

      <h3>Entra ID groups ({access.groups.length})</h3>
      <p className="admin-note">Everyone in a group gets the group's role. IT manages who is in each group in Entra ID.</p>
      <table className="versions-table">
        <thead>
          <tr>
            <th>Group</th>
            <th>Object ID</th>
            <th>Role</th>
            {canManage && <th />}
          </tr>
        </thead>
        <tbody>
          {access.groups.map((g) => (
            <tr key={g.group_id}>
              <td>
                <strong>{g.display_name}</strong>
              </td>
              <td>
                <code>{g.group_id}</code>
              </td>
              <td>
                {canManage ? (
                  <RoleSelect
                    label={`Role for ${g.display_name}`}
                    value={g.role}
                    disabled={isLastOwner(g.role)}
                    onChange={(role) => run(() => api.setGroupGrant(workspace.id, { ...g, role }), `${g.display_name} is now ${role}`)}
                  />
                ) : (
                  g.role
                )}
              </td>
              {canManage && (
                <td>
                  <button
                    className="action-btn"
                    disabled={isLastOwner(g.role)}
                    title={isLastOwner(g.role) ? LAST_OWNER_HELP : undefined}
                    onClick={() => run(() => api.removeGroupGrant(workspace.id, g.group_id), `Removed ${g.display_name}`)}
                  >
                    Remove
                  </button>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
      {canManage && (
        <form
          className="inline-form"
          aria-label="Add a group"
          onSubmit={(e) => {
            e.preventDefault();
            addGroup();
          }}
        >
          <label>
            Group object ID
            <input value={groupId} onChange={(e) => setGroupId(e.target.value)} placeholder="00000000-0000-0000-0000-000000000000" />
          </label>
          <label>
            Display name
            <input value={groupName} onChange={(e) => setGroupName(e.target.value)} placeholder="Legal editors" />
          </label>
          <label>
            Role
            <RoleSelect label="Role for the new group" value={groupRole} onChange={setGroupRole} />
          </label>
          <button className="test-button" type="submit">
            Add group
          </button>
        </form>
      )}

      <h3>People ({access.members.length})</h3>
      <table className="versions-table">
        <thead>
          <tr>
            <th>Person</th>
            <th>Role</th>
            {canManage && <th />}
          </tr>
        </thead>
        <tbody>
          {access.members.map((m) => (
            <tr key={m.subject}>
              <td>
                <strong>{m.subject}</strong>
              </td>
              <td>
                {canManage ? (
                  <RoleSelect
                    label={`Role for ${m.subject}`}
                    value={m.role}
                    disabled={isLastOwner(m.role)}
                    onChange={(role) => run(() => api.setMember(workspace.id, m.subject, role), `${m.subject} is now ${role}`)}
                  />
                ) : (
                  m.role
                )}
              </td>
              {canManage && (
                <td>
                  <button
                    className="action-btn"
                    disabled={isLastOwner(m.role)}
                    title={isLastOwner(m.role) ? LAST_OWNER_HELP : undefined}
                    onClick={() => run(() => api.removeMember(workspace.id, m.subject), `Removed ${m.subject}`)}
                  >
                    Remove
                  </button>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
      {canManage && (
        <form
          className="inline-form"
          aria-label="Add a person"
          onSubmit={(e) => {
            e.preventDefault();
            addPerson();
          }}
        >
          <label>
            Person (user ID)
            <input value={subject} onChange={(e) => setSubject(e.target.value)} placeholder="luis" />
          </label>
          <label>
            Role
            <RoleSelect label="Role for the new person" value={memberRole} onChange={setMemberRole} />
          </label>
          <button className="test-button" type="submit">
            Add person
          </button>
        </form>
      )}

      <ul className="role-help">
        {ROLES.map((r) => (
          <li key={r}>
            <strong>{r}</strong>: {ROLE_HELP[r]}
          </li>
        ))}
      </ul>
    </section>
  );
}
