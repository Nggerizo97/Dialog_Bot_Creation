import { useState, useEffect } from "react";
import {
  Bell,
  Bot,
  ChevronDown,
  CircleHelp,
  GitBranch,
  MoreHorizontal,
  PanelLeftClose,
  Play,
  Plus,
  Save,
  Send,
  Settings,
  Trash2,
  X,
  CheckCircle2,
} from "lucide-react";

export interface NodeItem {
  id: string;
  type: string;
  title: string;
  detail: string;
  properties?: Record<string, string>;
}

export interface VersionItem {
  id: string;
  version: string;
  status: "draft" | "published" | "archived";
  nodeCount: number;
  updatedAt: string;
}

const defaultNodes: NodeItem[] = [
  { id: "welcome", type: "Trigger", title: "Welcome", detail: "New conversation" },
  { id: "menu", type: "Menu", title: "What can we help with?", detail: "3 routes", properties: { prompt: "What can we help you with today?" } },
  { id: "balance", type: "Service", title: "Check balance", detail: "Accounts API", properties: { endpoint: "/mock/accounts/balance" } },
  { id: "handoff", type: "Response", title: "Connect to advisor", detail: "Text message", properties: { message: "Please hold while we connect you to an advisor." } },
];

export function App() {
  const [nodes, setNodes] = useState<NodeItem[]>(defaultNodes);
  const [selectedNodeId, setSelectedNodeId] = useState<string>("menu");
  const [published, setPublished] = useState<boolean>(false);
  const [activeTab, setActiveTab] = useState<"designer" | "versions" | "debugger">("designer");
  const [testOpen, setTestOpen] = useState<boolean>(false);
  const [testMessages, setTestMessages] = useState<Array<{ role: "bot" | "user"; text: string; options?: string[] }>>([
    { role: "bot", text: "Welcome to the Bot_Dialog_Generator demo!" },
    { role: "bot", text: "What can we help you with today?", options: ["Check balance", "Connect to advisor"] },
  ]);
  const [testInput, setTestInput] = useState<string>("");
  const [statusMessage, setStatusMessage] = useState<string>("");

  const [versions, setVersions] = useState<VersionItem[]>([
    { id: "v18", version: "v18", status: "draft", nodeCount: 4, updatedAt: "Just now" },
    { id: "v17", version: "v17", status: "published", nodeCount: 3, updatedAt: "Yesterday" },
  ]);
  const [activeVersion, setActiveVersion] = useState<string>("v17");

  // Attempt to load from Studio API if running
  useEffect(() => {
    fetch("http://localhost:8080/bots/retail-assistant/versions/v18")
      .then((res) => {
        if (!res.ok) throw new Error("API not ready");
        return res.json();
      })
      .then((data) => {
        if (data && data.nodes && data.nodes.length > 0) {
          setNodes(data.nodes);
          setSelectedNodeId(data.nodes[0].id);
        }
      })
      .catch(() => {
        // Fallback to default in-memory nodes
      });
  }, []);

  const selected = nodes.find((node) => node.id === selectedNodeId) || nodes[0] || {
    id: "none",
    type: "Response",
    title: "Select a node",
    detail: "",
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
    setNodes([...nodes, newNode]);
    setSelectedNodeId(newNode.id);
    setPublished(false);
    showStatus("New node added to draft");
  };

  const handleDeleteNode = (id: string) => {
    if (nodes.length <= 1) {
      alert("A flow must contain at least one node.");
      return;
    }
    const filtered = nodes.filter((n) => n.id !== id);
    setNodes(filtered);
    setSelectedNodeId(filtered[0].id);
    setPublished(false);
    showStatus(`Deleted node ${id}`);
  };

  const handleUpdateNode = (field: keyof NodeItem, value: any) => {
    setNodes(
      nodes.map((n) => {
        if (n.id === selectedNodeId) {
          return { ...n, [field]: value };
        }
        return n;
      })
    );
    setPublished(false);
  };

  const handleSaveDraft = async () => {
    try {
      await fetch("http://localhost:8080/bots/retail-assistant/versions/v18", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ nodes, edges: [] }),
      });
    } catch {
      // Continue locally
    }
    showStatus("Draft v18 saved successfully");
  };

  const handlePublish = async () => {
    try {
      const res = await fetch("http://localhost:8080/bots/retail-assistant/versions/v18/publish", {
        method: "POST",
      });
      if (res.ok) {
        setPublished(true);
        setActiveVersion("v18");
        setVersions(
          versions.map((v) => (v.id === "v18" ? { ...v, status: "published" } : v))
        );
        showStatus("Published v18 as active bot version!");
        return;
      }
    } catch {
      // Fallback local publishing
    }
    setPublished(true);
    setActiveVersion("v18");
    showStatus("Published v18 successfully!");
  };

  const handleSendTestMessage = (textToSend?: string) => {
    const text = textToSend || testInput.trim();
    if (!text) return;

    const newMessages = [...testMessages, { role: "user" as const, text }];
    setTestMessages(newMessages);
    if (!textToSend) setTestInput("");

    setTimeout(() => {
      if (text.toLowerCase().includes("balance")) {
        setTestMessages([
          ...newMessages,
          { role: "bot", text: "Your current checking balance is $1,250.50 USD." },
        ]);
      } else if (text.toLowerCase().includes("advisor")) {
        setTestMessages([
          ...newMessages,
          { role: "bot", text: "Connecting you with an available advisor now..." },
        ]);
      } else {
        setTestMessages([
          ...newMessages,
          { role: "bot", text: `Echo: received '${text}'. How else can Bot_Dialog_Generator assist?` },
        ]);
      }
    }, 400);
  };

  const showStatus = (msg: string) => {
    setStatusMessage(msg);
    setTimeout(() => setStatusMessage(""), 3500);
  };

  return (
    <main className="studio-shell">
      <header className="topbar">
        <div className="brand">
          <Bot size={22} strokeWidth={2.4} /> Bot_Dialog_Generator <span>Studio</span>
          <span className="prototype-badge">PROTOTYPE</span>
        </div>
        <button className="project-switcher" aria-label="Choose application">
          Retail assistant <ChevronDown size={15} />
        </button>
        <div className="version">
          <GitBranch size={15} /> draft / v18 (active: {activeVersion})
        </div>
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
            className={`publish ${published ? "published" : ""}`}
            onClick={handlePublish}
            aria-label="Publish bot"
          >
            <Send size={16} /> {published ? "Published" : "Publish"}
          </button>
        </div>
      </header>

      <aside className="sidebar">
        <button className="icon-button collapse" aria-label="Collapse navigation" title="Collapse navigation">
          <PanelLeftClose size={18} />
        </button>
        <nav>
          <a
            className={activeTab === "designer" ? "active" : ""}
            onClick={() => setActiveTab("designer")}
          >
            <Bot size={18} /> Designer
          </a>
          <a
            className={activeTab === "versions" ? "active" : ""}
            onClick={() => setActiveTab("versions")}
          >
            <GitBranch size={18} /> Versions
          </a>
          <a
            className={activeTab === "debugger" ? "active" : ""}
            onClick={() => setActiveTab("debugger")}
          >
            <CircleHelp size={18} /> Debugger
          </a>
        </nav>
        <button
          className="new-flow"
          onClick={() => {
            handleAddNode();
            showStatus("Created new dialog branch");
          }}
        >
          <Plus size={17} /> New dialog
        </button>
        <div className="sidebar-footer">
          Production<br />
          <strong>us-east-1</strong>
        </div>
      </aside>

      {activeTab === "designer" ? (
        <section className="workspace">
          <div className="workspace-header">
            <div>
              <p className="eyebrow">Main flow</p>
              <h1>Customer support</h1>
            </div>
            <div className="workspace-tools">
              <button
                className="icon-button"
                aria-label="Save draft"
                title="Save draft"
                onClick={handleSaveDraft}
              >
                <Save size={18} />
              </button>
              <button
                className="test-button"
                onClick={() => setTestOpen(true)}
              >
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
                className={`flow-node ${node.type.toLowerCase()} ${
                  selectedNodeId === node.id ? "selected" : ""
                } node-${index}`}
                key={node.id}
                onClick={() => setSelectedNodeId(node.id)}
              >
                <span className="node-type">{node.type}</span>
                <strong>{node.title}</strong>
                <small>{node.detail}</small>
              </button>
            ))}
            <button
              className="add-node"
              aria-label="Add node"
              title="Add node"
              onClick={handleAddNode}
            >
              <Plus size={19} />
            </button>
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
                          <button
                            key={opt}
                            className="test-menu-btn"
                            onClick={() => handleSendTestMessage(opt)}
                          >
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
                <input
                  value={testInput}
                  onChange={(e) => setTestInput(e.target.value)}
                  placeholder="Type test input..."
                />
                <button type="submit">Send</button>
              </form>
            </div>
          )}
        </section>
      ) : activeTab === "versions" ? (
        <section className="workspace versions-view">
          <h2>Version History & Deployment Pointers</h2>
          <p className="eyebrow" style={{ marginTop: 10 }}>Bot: Retail assistant</p>
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
                    <span className={`version-badge ${ver.status}`}>
                      {ver.status.toUpperCase()}
                    </span>
                  </td>
                  <td>{ver.nodeCount}</td>
                  <td>{ver.updatedAt}</td>
                  <td>
                    {activeVersion === ver.id ? (
                      <strong style={{ color: "#15766b" }}>Active Pointer</strong>
                    ) : (
                      "-"
                    )}
                  </td>
                  <td>
                    {activeVersion !== ver.id && ver.status === "published" && (
                      <button
                        className="action-btn"
                        onClick={() => {
                          setActiveVersion(ver.id);
                          showStatus(`Switched active version to ${ver.id}`);
                        }}
                      >
                        Rollback to {ver.id}
                      </button>
                    )}
                    {ver.status === "draft" && (
                      <button
                        className="action-btn"
                        onClick={() => setActiveTab("designer")}
                      >
                        Edit Draft
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

      {activeTab === "designer" && (
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
          <label>
            Node name
            <input
              value={selected.title}
              onChange={(e) => handleUpdateNode("title", e.target.value)}
            />
          </label>
          <label>
            Node Type
            <select
              value={selected.type}
              onChange={(e) => handleUpdateNode("type", e.target.value)}
            >
              <option value="Trigger">Trigger</option>
              <option value="Response">Response</option>
              <option value="Menu">Menu</option>
              <option value="Service">Service</option>
              <option value="Jump">Jump</option>
            </select>
          </label>
          <label>
            Message / Prompt
            <textarea
              value={selected.detail}
              onChange={(e) => handleUpdateNode("detail", e.target.value)}
            />
          </label>
          <label>
            Fallback route
            <select defaultValue="handoff">
              <option value="handoff">Connect to advisor</option>
              <option value="end">End conversation</option>
            </select>
          </label>

          <button
            className="delete-node-btn"
            onClick={() => handleDeleteNode(selected.id)}
          >
            <Trash2 size={14} style={{ marginRight: 6, verticalAlign: "middle" }} />
            Delete this node
          </button>

          <div className="inspector-note">
            <span />
            Changes are saved to draft version v18.
          </div>
        </aside>
      )}
    </main>
  );
}