import { useEffect, useMemo, useState } from "react";
import {
  Bell,
  Bot,
  CircleHelp,
  GitBranch,
  LogOut,
  MoreHorizontal,
  PanelLeftClose,
  Play,
  Plus,
  Save,
  Send,
  Settings,
  ShieldCheck,
  Trash2,
  X,
  CheckCircle2,
} from "lucide-react";
import {
  ApiError,
  createApi,
  loadSession,
  saveSession,
  type Bot as BotRecord,
  type Me,
  type NodeItem,
  type Role,
  type Session,
  type StudioApi,
  type Version,
} from "./api";
import { Login } from "./Login";
import { AdminView } from "./AdminView";

export type { NodeItem } from "./api";

const canEdit = (role?: Role) => role === "editor" || role === "owner";
const canPublish = (role?: Role) => role === "owner";

export function App() {
  const [session, setSession] = useState<Session | null>(() => loadSession());
  const api = useMemo(() => (session ? createApi(session.token) : null), [session]);

  if (!session || !api) {
    return (
      <Login
        onSignedIn={(s) => {
          saveSession(s);
          setSession(s);
        }}
      />
    );
  }
  return (
    <Studio
      api={api}
      onSignOut={() => {
        saveSession(null);
        setSession(null);
      }}
    />
  );
}

type Tab = "designer" | "versions" | "debugger" | "admin";

export function Studio({ api, onSignOut }: { api: StudioApi; onSignOut: () => void }) {
  const [me, setMe] = useState<Me | null>(null);
  const [workspaceId, setWorkspaceId] = useState("");
  const [bots, setBots] = useState<BotRecord[]>([]);
  const [botId, setBotId] = useState("");
  const [draft, setDraft] = useState<Version | null>(null);
  const [nodes, setNodes] = useState<NodeItem[]>([]);
  const [selectedNodeId, setSelectedNodeId] = useState("");
  const [versions, setVersions] = useState<Version[]>([]);
  const [dirty, setDirty] = useState(false);
  const [activeTab, setActiveTab] = useState<Tab>("designer");
  const [error, setError] = useState("");
  const [testOpen, setTestOpen] = useState<boolean>(false);
  const [testMessages, setTestMessages] = useState<Array<{ role: "bot" | "user"; text: string; options?: string[] }>>([
    { role: "bot", text: "Welcome to the Bot_Dialog_Generator demo!" },
    { role: "bot", text: "What can we help you with today?", options: ["Check balance", "Connect to advisor"] },
  ]);
  const [testInput, setTestInput] = useState<string>("");
  const [statusMessage, setStatusMessage] = useState<string>("");

  const workspace = me?.workspaces.find((w) => w.id === workspaceId);
  const role = workspace?.role;
  const bot = bots.find((b) => b.id === botId);
  const editable = canEdit(role);

  const fail = (e: unknown) => {
    if (e instanceof ApiError && e.status === 401) {
      onSignOut();
      return;
    }
    setError(e instanceof Error ? e.message : String(e));
  };

  const showStatus = (msg: string) => {
    setStatusMessage(msg);
    setTimeout(() => setStatusMessage(""), 3500);
  };

  useEffect(() => {
    api
      .me()
      .then((m) => {
        setMe(m);
        if (m.workspaces.length > 0) setWorkspaceId(m.workspaces[0].id);
        else if (m.platform_admin) setActiveTab("admin");
      })
      .catch(fail);
  }, [api]);

  useEffect(() => {
    if (!workspaceId) return;
    api
      .listBots(workspaceId)
      .then((list) => {
        setBots(list);
        setBotId(list[0]?.id ?? "");
      })
      .catch(fail);
  }, [api, workspaceId]);

  useEffect(() => {
    if (!bot) {
      setDraft(null);
      setNodes([]);
      setVersions([]);
      return;
    }
    Promise.all([api.getVersion(bot.workspace_id, bot.id, bot.draft_version), api.listVersions(bot.workspace_id, bot.id)])
      .then(([ver, list]) => {
        setDraft(ver);
        setNodes(ver.nodes ?? []);
        setSelectedNodeId(ver.nodes?.[0]?.id ?? "");
        setVersions(list);
        setDirty(false);
      })
      .catch(fail);
  }, [api, bot?.id, bot?.draft_version]);

  const refreshBots = async () => setBots(await api.listBots(workspaceId));

  const selected = nodes.find((node) => node.id === selectedNodeId) ||
    nodes[0] || { id: "none", type: "Response", title: "Select a node", detail: "" };

  const editNodes = (next: NodeItem[]) => {
    setNodes(next);
    setDirty(true);
  };

  const handleAddNode = () => {
    const nextIdx = nodes.length + 1;
    const newNode: NodeItem = {
      id: `node_${nextIdx}`,
      type: "Response",
      title: `Response Step ${nextIdx}`,
      detail: "Text message",
      properties: { message: "Thank you for reaching out." },
    };
    editNodes([...nodes, newNode]);
    setSelectedNodeId(newNode.id);
    showStatus("New node added to draft");
  };

  const handleDeleteNode = (id: string) => {
    if (nodes.length <= 1) {
      alert("A flow must contain at least one node.");
      return;
    }
    const filtered = nodes.filter((n) => n.id !== id);
    editNodes(filtered);
    setSelectedNodeId(filtered[0].id);
    showStatus(`Deleted node ${id}`);
  };

  const handleUpdateNode = (field: keyof NodeItem, value: string) => {
    editNodes(nodes.map((n) => (n.id === selectedNodeId ? { ...n, [field]: value } : n)));
  };

  // Saves the edited nodes. Edges come from the loaded draft (the canvas does not edit
  // them yet); edges that point at deleted nodes are dropped.
  const persistDraft = async () => {
    if (!bot || !draft) return null;
    const ids = new Set(nodes.map((n) => n.id));
    const edges = (draft.edges ?? []).filter((e) => ids.has(e.from) && ids.has(e.to));
    const saved = await api.saveDraft(workspaceId, bot.id, draft.version, {
      entry_node_id: draft.entry_node_id,
      nodes,
      edges,
    });
    setDraft(saved);
    setDirty(false);
    return saved;
  };

  const handleSaveDraft = async () => {
    try {
      const saved = await persistDraft();
      if (saved) showStatus(`Draft ${saved.version} saved`);
    } catch (e) {
      fail(e);
    }
  };

  const handlePublish = async () => {
    if (!bot || !draft) return;
    try {
      if (dirty) await persistDraft();
      const result = await api.publish(workspaceId, bot.id, draft.version);
      await api.createDraft(workspaceId, bot.id, result.version);
      await refreshBots();
      showStatus(`Published ${result.version}. A new draft is ready for edits.`);
    } catch (e) {
      fail(e);
    }
  };

  const handleActivate = async (version: string) => {
    if (!bot) return;
    try {
      await api.setActiveVersion(workspaceId, bot.id, version);
      await refreshBots();
      showStatus(`Active version is now ${version}`);
    } catch (e) {
      fail(e);
    }
  };

  const handleNewBot = async () => {
    const name = window.prompt("Name of the new bot");
    if (!name?.trim()) return;
    try {
      const created = await api.createBot(workspaceId, name.trim());
      await refreshBots();
      setBotId(created.id);
      showStatus(`Created ${created.name}`);
    } catch (e) {
      fail(e);
    }
  };

  const handleSendTestMessage = (textToSend?: string) => {
    const text = textToSend || testInput.trim();
    if (!text) return;

    const newMessages = [...testMessages, { role: "user" as const, text }];
    setTestMessages(newMessages);
    if (!textToSend) setTestInput("");

    setTimeout(() => {
      if (text.toLowerCase().includes("balance")) {
        setTestMessages([...newMessages, { role: "bot", text: "Your current checking balance is $1,250.50 USD." }]);
      } else if (/person|agent/i.test(text)) {
        setTestMessages([...newMessages, { role: "bot", text: "Connecting you with an available agent from the contact center now..." }]);
      } else if (/ai assistant/i.test(text)) {
        setTestMessages([
          ...newMessages,
          { role: "bot", text: "You're chatting with the AI assistant. AI answers are not connected in this demo yet." },
          { role: "bot", text: "You can talk to a person at any time.", options: ["Talk to a person"] },
        ]);
      } else if (text.toLowerCase().includes("advisor")) {
        // Ask before transferring: AI assistant first, or a person from the contact center.
        setTestMessages([
          ...newMessages,
          {
            role: "bot",
            text: "Would you like to chat with our AI assistant or talk to a person from the contact center?",
            options: ["AI assistant", "Contact center agent"],
          },
        ]);
      } else {
        setTestMessages([
          ...newMessages,
          { role: "bot", text: `Echo: received '${text}'. How else can Bot_Dialog_Generator assist?` },
        ]);
      }
    }, 400);
  };

  const publishTitle = !canPublish(role) ? "Only workspace owners can publish" : "Publish the draft as the active version";
  const noWorkspace = me !== null && me.workspaces.length === 0;

  return (
    <main className="studio-shell">
      <header className="topbar">
        <div className="brand">
          <Bot size={22} strokeWidth={2.4} /> Bot_Dialog_Generator <span>Studio</span>
          <span className="prototype-badge">PROTOTYPE</span>
        </div>
        <div className="pickers">
          {me && me.workspaces.length > 0 && (
            <select
              className="picker"
              aria-label="Workspace"
              value={workspaceId}
              onChange={(e) => {
                setWorkspaceId(e.target.value);
                setActiveTab("designer");
              }}
            >
              {me.workspaces.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.name}
                </option>
              ))}
            </select>
          )}
          {bots.length > 0 && (
            <select className="picker" aria-label="Bot" value={botId} onChange={(e) => setBotId(e.target.value)}>
              {bots.map((b) => (
                <option key={b.id} value={b.id}>
                  {b.name}
                </option>
              ))}
            </select>
          )}
          {workspaceId && editable && (
            <button className="icon-button" aria-label="New bot" title="New bot" onClick={handleNewBot}>
              <Plus size={17} />
            </button>
          )}
          {role && <span className="role-pill">{role}</span>}
        </div>
        {draft && (
          <div className="version">
            <GitBranch size={15} /> draft / {draft.version} (active: {bot?.active_version ?? "none"})
          </div>
        )}
        <div className="topbar-actions">
          {statusMessage && (
            <span style={{ fontSize: 11, color: "#a8d8ce", display: "flex", alignItems: "center", gap: 5 }}>
              <CheckCircle2 size={13} /> {statusMessage}
            </span>
          )}
          <button className="icon-button" aria-label="Notifications" title="Notifications">
            <Bell size={18} />
          </button>
          <button className="icon-button" aria-label="Settings" title="Settings">
            <Settings size={18} />
          </button>
          <button
            className="publish"
            onClick={handlePublish}
            aria-label="Publish bot"
            title={publishTitle}
            disabled={!canPublish(role) || !draft}
          >
            <Send size={16} /> Publish
          </button>
          {me && (
            <span className="user-chip" title={me.email || me.subject}>
              {me.platform_admin && <ShieldCheck size={14} aria-label="Platform admin" />}
              {me.subject}
            </span>
          )}
          <button className="icon-button" aria-label="Sign out" title="Sign out" onClick={onSignOut}>
            <LogOut size={17} />
          </button>
        </div>
      </header>

      <aside className="sidebar">
        <button className="icon-button collapse" aria-label="Collapse navigation" title="Collapse navigation">
          <PanelLeftClose size={18} />
        </button>
        <nav>
          {workspace && (
            <>
              <a className={activeTab === "designer" ? "active" : ""} onClick={() => setActiveTab("designer")}>
                <Bot size={18} /> Designer
              </a>
              <a className={activeTab === "versions" ? "active" : ""} onClick={() => setActiveTab("versions")}>
                <GitBranch size={18} /> Versions
              </a>
              <a className={activeTab === "debugger" ? "active" : ""} onClick={() => setActiveTab("debugger")}>
                <CircleHelp size={18} /> Debugger
              </a>
            </>
          )}
          {me?.platform_admin && (
            <a className={activeTab === "admin" ? "active" : ""} onClick={() => setActiveTab("admin")}>
              <ShieldCheck size={18} /> Admin
            </a>
          )}
        </nav>
        {workspace && (
          <button
            className="new-flow"
            disabled={!editable}
            onClick={() => {
              handleAddNode();
              showStatus("Created new dialog branch");
            }}
          >
            <Plus size={17} /> New dialog
          </button>
        )}
        <div className="sidebar-footer">
          Workspace
          <br />
          <strong>{workspace?.name ?? "—"}</strong>
        </div>
      </aside>

      {activeTab === "admin" && me?.platform_admin ? (
        <AdminView api={api} onError={fail} />
      ) : noWorkspace ? (
        <section className="workspace versions-view">
          <div className="empty-state">
            <h2>No workspace yet</h2>
            <p>You are not a member of any workspace. Ask a workspace owner or a platform admin to add you.</p>
          </div>
        </section>
      ) : workspace && bots.length === 0 ? (
        <section className="workspace versions-view">
          <div className="empty-state">
            <h2>No bots in {workspace.name}</h2>
            <p>{editable ? "Create the first bot for your area." : "An editor or owner can create the first bot."}</p>
            {editable && (
              <button className="test-button" onClick={handleNewBot}>
                <Plus size={16} /> New bot
              </button>
            )}
          </div>
        </section>
      ) : activeTab === "designer" ? (
        <section className="workspace">
          <div className="workspace-header">
            <div>
              <p className="eyebrow">{workspace?.name ?? "Main flow"}</p>
              <h1>{bot?.name ?? "Loading…"}</h1>
            </div>
            <div className="workspace-tools">
              <button
                className="icon-button"
                aria-label="Save draft"
                title={editable ? "Save draft" : "Your role is read-only"}
                onClick={handleSaveDraft}
                disabled={!editable || !draft}
              >
                <Save size={18} />
              </button>
              <button className="test-button" onClick={() => setTestOpen(true)}>
                <Play size={16} /> Test
              </button>
            </div>
          </div>
          <div className="canvas" aria-label="Conversation flow canvas">
            <div className="connector line-one" />
            <div className="connector line-two" />
            <div className="connector line-three" />
            {nodes.map((node, index) => (
              <button
                className={`flow-node ${node.type.toLowerCase()} ${selectedNodeId === node.id ? "selected" : ""} node-${index}`}
                key={node.id}
                onClick={() => setSelectedNodeId(node.id)}
              >
                <span className="node-type">{node.type}</span>
                <strong>{node.title}</strong>
                <small>{node.detail}</small>
              </button>
            ))}
            {editable && (
              <button className="add-node" aria-label="Add node" title="Add node" onClick={handleAddNode}>
                <Plus size={19} />
              </button>
            )}
          </div>

          {testOpen && (
            <div className="test-drawer">
              <div className="test-header">
                <span>Interactive Test Chat</span>
                <button
                  onClick={() => setTestOpen(false)}
                  style={{ background: "none", border: 0, color: "#fff", cursor: "pointer" }}
                >
                  <X size={18} />
                </button>
              </div>
              <div className="test-messages">
                {testMessages.map((m, idx) => (
                  <div key={idx} className={`test-msg ${m.role}`}>
                    <div>{m.text}</div>
                    {m.options && (
                      <div>
                        {m.options.map((opt) => (
                          <button key={opt} className="test-menu-btn" onClick={() => handleSendTestMessage(opt)}>
                            {opt}
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                ))}
              </div>
              <form
                className="test-input-form"
                onSubmit={(e) => {
                  e.preventDefault();
                  handleSendTestMessage();
                }}
              >
                <input value={testInput} onChange={(e) => setTestInput(e.target.value)} placeholder="Type test input..." />
                <button type="submit">Send</button>
              </form>
            </div>
          )}
        </section>
      ) : activeTab === "versions" ? (
        <section className="workspace versions-view">
          <h2>Version History & Deployment Pointers</h2>
          <p className="eyebrow" style={{ marginTop: 10 }}>
            Bot: {bot?.name}
          </p>
          <table className="versions-table">
            <thead>
              <tr>
                <th>Version</th>
                <th>Status</th>
                <th>Nodes</th>
                <th>Last Updated</th>
                <th>Active Pointer</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {versions.map((ver) => (
                <tr key={ver.id}>
                  <td>
                    <strong>{ver.version}</strong>
                  </td>
                  <td>
                    <span className={`version-badge ${ver.status}`}>{ver.status.toUpperCase()}</span>
                  </td>
                  <td>{ver.nodes?.length ?? 0}</td>
                  <td>{new Date(ver.updated_at).toLocaleString()}</td>
                  <td>
                    {bot?.active_version === ver.version ? (
                      <strong style={{ color: "#15766b" }}>Active Pointer</strong>
                    ) : (
                      "-"
                    )}
                  </td>
                  <td>
                    {bot?.active_version !== ver.version && ver.status === "published" && canPublish(role) && (
                      <button className="action-btn" onClick={() => handleActivate(ver.version)}>
                        Roll back to {ver.version}
                      </button>
                    )}
                    {ver.status === "draft" && ver.version === bot?.draft_version && (
                      <button className="action-btn" onClick={() => setActiveTab("designer")}>
                        {editable ? "Edit Draft" : "View Draft"}
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      ) : (
        <section className="workspace versions-view">
          <h2>Conversation Debugger & Execution Traces</h2>
          <p style={{ marginTop: 10, color: "#666" }}>
            Traces will record live turn transitions once the engine Kafka consumer group is active.
          </p>
        </section>
      )}

      {activeTab === "designer" && workspace && bots.length > 0 && (
        <aside className="inspector">
          <div className="inspector-title">
            <div>
              <p className="eyebrow">Selected node</p>
              <h2>{selected.type}</h2>
            </div>
            <button className="icon-button" aria-label="More options" title="More options">
              <MoreHorizontal size={19} />
            </button>
          </div>
          <fieldset className="inspector-fields" disabled={!editable}>
            <label>
              Node name
              <input value={selected.title} onChange={(e) => handleUpdateNode("title", e.target.value)} />
            </label>
            <label>
              Node Type
              <select value={selected.type} onChange={(e) => handleUpdateNode("type", e.target.value)}>
                <option value="Trigger">Trigger</option>
                <option value="Response">Response</option>
                <option value="Menu">Menu</option>
                <option value="Service">Service</option>
                <option value="Jump">Jump</option>
              </select>
            </label>
            <label>
              Message / Prompt
              <textarea value={selected.detail ?? ""} onChange={(e) => handleUpdateNode("detail", e.target.value)} />
            </label>
            <label>
              Fallback route
              <select defaultValue="handoff">
                <option value="handoff">Connect to advisor</option>
                <option value="end">End conversation</option>
              </select>
            </label>

            <button className="delete-node-btn" onClick={() => handleDeleteNode(selected.id)}>
              <Trash2 size={14} style={{ marginRight: 6, verticalAlign: "middle" }} />
              Delete this node
            </button>
          </fieldset>

          <div className="inspector-note">
            <span />
            {editable
              ? `Changes are saved to draft version ${draft?.version ?? ""}${dirty ? " (unsaved changes)" : ""}.`
              : "Your role in this workspace is read-only."}
          </div>
        </aside>
      )}

      {error && (
        <div className="error-toast" role="alert">
          {error}
          <button aria-label="Dismiss error" onClick={() => setError("")}>
            <X size={14} />
          </button>
        </div>
      )}
    </main>
  );
}
