import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent } from "react";
import { Bot, CircleHelp, GitBranch, Lock, LogOut, Play, Plus, Save, Send, ShieldCheck, Trash2, Users, X, CheckCircle2 } from "lucide-react";
import {
  ApiError,
  createApi,
  loadSession,
  pendingTerms,
  saveSession,
  setPendingTerms,
  type Audience,
  type Bot as BotRecord,
  type Me,
  type NodeItem,
  type Role,
  type Session,
  type StudioApi,
  type Version,
} from "./api";
import { Login } from "./Login";
import { createCompanySignIn, oidcSettings, type CompanySignIn } from "./companySignIn";
import { cognitoSettings, createCognitoSignIn, type CognitoSignIn } from "./cognitoSignIn";
import { AdminView } from "./AdminView";
import { MembersView } from "./MembersView";
import { PrivacyView } from "./PrivacyView";
import { Footer } from "./Footer";
import { LegalPage } from "./LegalPage";
import { Dialog, useFocusTrap } from "./Dialog";
import { PrivacyBanner, PrivacyChoicesDialog } from "./PrivacyBanner";
import { legalDocumentForHash } from "./legal";
import { loadChoices, saveChoices, track, type PrivacyChoices, type StudioEvent } from "./privacyChoices";

export type { NodeItem } from "./api";

const canEdit = (role?: Role) => role === "editor" || role === "owner";
const canPublish = (role?: Role) => role === "owner";

let defaultCompanySignIn: CompanySignIn | null | undefined;

/** Company sign-in when VITE_OIDC_* is configured, otherwise null (development sign-in). */
function configuredCompanySignIn(): CompanySignIn | null {
  if (defaultCompanySignIn === undefined) {
    const settings = oidcSettings();
    defaultCompanySignIn = settings ? createCompanySignIn(settings) : null;
  }
  return defaultCompanySignIn;
}

function useHash(): string {
  const [hash, setHash] = useState(() => window.location.hash);
  useEffect(() => {
    const update = () => setHash(window.location.hash);
    window.addEventListener("hashchange", update);
    return () => window.removeEventListener("hashchange", update);
  }, []);
  return hash;
}

/** Moves keyboard focus to the page's main content, without changing the address. */
function skipToContent(event: MouseEvent<HTMLAnchorElement>) {
  event.preventDefault();
  document.getElementById("main-content")?.focus();
}

let defaultCognitoSignIn: CognitoSignIn | null | undefined;

/** Password sign-in against a local Cognito (AWS Emulator) when VITE_COGNITO_* is set. */
function configuredCognitoSignIn(): CognitoSignIn | null {
  if (defaultCognitoSignIn === undefined) {
    const settings = cognitoSettings();
    defaultCognitoSignIn = settings ? createCognitoSignIn(settings) : null;
  }
  return defaultCognitoSignIn;
}

export function App({
  companySignIn = configuredCompanySignIn(),
  cognitoSignIn = companySignIn ? null : configuredCognitoSignIn(),
  analyticsUrl = import.meta.env.VITE_ANALYTICS_URL,
}: { companySignIn?: CompanySignIn | null; cognitoSignIn?: CognitoSignIn | null; analyticsUrl?: string } = {}) {
  const hash = useHash();
  const [choices, setChoices] = useState<PrivacyChoices | null>(() => loadChoices());
  const [choicesOpen, setChoicesOpen] = useState(false);
  const decide = (analytics: boolean) => {
    setChoices(saveChoices(analytics));
    setChoicesOpen(false);
  };
  const onTrack = useCallback((event: StudioEvent, props?: { tab?: string }) => track(analyticsUrl, choices, event, props), [analyticsUrl, choices]);

  const legal = legalDocumentForHash(hash);
  const page = legal !== undefined ? <LegalPage doc={legal} /> : <Gate companySignIn={companySignIn} cognitoSignIn={cognitoSignIn} onTrack={onTrack} />;

  return (
    <div className="app-frame">
      <a className="skip-link" href="#main-content" onClick={skipToContent}>
        Skip to main content
      </a>
      {page}
      <Footer onPrivacyChoices={analyticsUrl ? () => setChoicesOpen(true) : undefined} />
      {analyticsUrl && !choices && !choicesOpen && <PrivacyBanner onDecide={decide} onCustomize={() => setChoicesOpen(true)} />}
      {choicesOpen && <PrivacyChoicesDialog current={choices} onSave={decide} onClose={() => setChoicesOpen(false)} />}
    </div>
  );
}

/** Signs the person in, then shows the studio. */
function Gate({
  companySignIn,
  cognitoSignIn,
  onTrack,
}: {
  companySignIn: CompanySignIn | null;
  cognitoSignIn: CognitoSignIn | null;
  onTrack: (event: StudioEvent, props?: { tab?: string }) => void;
}) {
  // Company sign-in (oidc-client-ts) and Cognito keep their own sessions; otherwise the dev session store does.
  const external = companySignIn !== null || cognitoSignIn !== null;
  const [session, setSession] = useState<Session | null>(() => (external ? null : loadSession()));
  const [ready, setReady] = useState(!external);

  useEffect(() => {
    if (!cognitoSignIn) return;
    let active = true;
    cognitoSignIn
      .restore()
      .then((s) => active && setSession(s))
      .finally(() => active && setReady(true));
    const unsubscribe = cognitoSignIn.onRenewed((s) => setSession(s));
    return () => {
      active = false;
      unsubscribe();
    };
  }, [cognitoSignIn]);
  const [signInError, setSignInError] = useState("");
  const api = useMemo(() => (session ? createApi(session.token) : null), [session]);

  useEffect(() => {
    if (!companySignIn) return;
    let active = true;
    companySignIn
      .complete()
      .then((s) => s ?? companySignIn.restore())
      .then((s) => active && setSession(s))
      .catch((e) => active && setSignInError(e instanceof Error ? e.message : String(e)))
      .finally(() => active && setReady(true));
    const unsubscribe = companySignIn.onRenewed((s) => setSession(s));
    return () => {
      active = false;
      unsubscribe();
    };
  }, [companySignIn]);

  if (!ready) {
    return (
      <main id="main-content" className="login-page" tabIndex={-1}>
        <p role="status">Signing you in…</p>
      </main>
    );
  }
  if (!session || !api) {
    return (
      <Login
        onSignedIn={(s) => {
          // Cognito keeps its own session; only development sign-in uses the dev session store.
          if (!cognitoSignIn) saveSession(s);
          setSession(s);
        }}
        startCompanySignIn={companySignIn ? () => companySignIn.start() : undefined}
        passwordSignIn={cognitoSignIn ? (username, password) => cognitoSignIn.signIn(username, password) : undefined}
        initialError={signInError}
      />
    );
  }
  return (
    <Studio
      api={api}
      onTrack={onTrack}
      onSignOut={() => {
        if (companySignIn) void companySignIn.signOut();
        else if (cognitoSignIn) cognitoSignIn.signOut();
        else saveSession(null);
        setPendingTerms(null);
        setSession(null);
      }}
    />
  );
}

type Tab = "designer" | "versions" | "debugger" | "members" | "privacy" | "admin";

// The test chat answers with sample replies until it is connected to the engine
// (PLAN milestone 3). They say so, rather than pretending to check an account or call an agent.
const SAMPLE_BALANCE = "Sample data: a checking balance of $1,250.50 USD. The test chat is not connected to any real account.";
const SAMPLE_HANDOFF = "The test chat has no contact center, so no agent will join. A published bot would hand the conversation to a person here.";

export function Studio({
  api,
  onSignOut,
  onTrack = () => {},
}: {
  api: StudioApi;
  onSignOut: () => void;
  onTrack?: (event: StudioEvent, props?: { tab?: string }) => void;
}) {
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
  const [confirmMinors, setConfirmMinors] = useState(false);
  const [guardianConfirmed, setGuardianConfirmed] = useState(false);
  const [audience, setAudience] = useState<Audience>("general");
  const [minAge, setMinAge] = useState("0");
  const testDrawer = useRef<HTMLDivElement>(null);

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

  // Record the terms accepted on the sign-in page, once, now that the user is known.
  useEffect(() => {
    const version = pendingTerms();
    if (!version) return;
    api
      .acceptTerms(version)
      .then(() => setPendingTerms(null))
      .catch(fail);
  }, [api]);

  useEffect(() => {
    onTrack("tab_viewed", { tab: activeTab });
  }, [activeTab]);

  useEffect(() => {
    setAudience(bot?.audience ?? "general");
    setMinAge(String(bot?.min_age ?? 0));
  }, [bot?.id, bot?.audience, bot?.min_age]);

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
      if (saved) {
        showStatus(`Draft ${saved.version} saved`);
        onTrack("draft_saved");
      }
    } catch (e) {
      fail(e);
    }
  };

  const publish = async (parentalConsentConfirmed = false) => {
    if (!bot || !draft) return;
    setConfirmMinors(false);
    try {
      if (dirty) await persistDraft();
      const result = await api.publish(workspaceId, bot.id, draft.version, { parentalConsentConfirmed });
      await api.createDraft(workspaceId, bot.id, result.version);
      await refreshBots();
      showStatus(`Published ${result.version}. A new draft is ready for edits.`);
      onTrack("bot_published");
    } catch (e) {
      fail(e);
    }
  };

  // A bot for children goes live only once its owner confirms a parental-consent process exists.
  const handlePublish = () => {
    if (bot?.audience === "minors") {
      setGuardianConfirmed(false);
      setConfirmMinors(true);
      return;
    }
    void publish();
  };

  const handleSaveAudience = async () => {
    if (!bot) return;
    const age = Number.parseInt(minAge, 10);
    if (!Number.isInteger(age) || age < 0 || age > 21) {
      setError("Minimum age must be a whole number from 0 to 21.");
      return;
    }
    try {
      await api.updateBotSettings(workspaceId, bot.id, { audience, min_age: age });
      await refreshBots();
      showStatus(audience === "minors" ? "Saved. The chat will ask a parent or guardian for permission." : "Saved who the bot is for");
    } catch (e) {
      fail(e);
    }
  };

  const closeTest = useCallback(() => setTestOpen(false), []);
  useFocusTrap(testDrawer, closeTest, { active: testOpen });

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
        setTestMessages([...newMessages, { role: "bot", text: SAMPLE_BALANCE }]);
      } else if (/person|agent/i.test(text)) {
        setTestMessages([...newMessages, { role: "bot", text: SAMPLE_HANDOFF }]);
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

  const showInspector = activeTab === "designer" && !!workspace && bots.length > 0;

  const tab = (id: Tab, label: string, Icon: typeof Bot) => (
    <button type="button" className={activeTab === id ? "active" : ""} aria-current={activeTab === id ? "page" : undefined} onClick={() => setActiveTab(id)}>
      <Icon size={18} aria-hidden="true" /> {label}
    </button>
  );

  return (
    <div className="studio-shell">
      <header className="topbar">
        <div className="brand">
          <Bot size={22} strokeWidth={2.4} aria-hidden="true" /> Bot_Dialog_Generator <span>Studio</span>
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
              <Plus size={17} aria-hidden="true" />
            </button>
          )}
          {role && <span className="role-pill">{role}</span>}
        </div>
        {draft && (
          <div className="version">
            <GitBranch size={15} aria-hidden="true" /> draft / {draft.version} (active: {bot?.active_version ?? "none"})
          </div>
        )}
        <div className="topbar-actions">
          {/* Always present, so screen readers announce each new status. */}
          <span className="status-message" role="status">
            {statusMessage && (
              <>
                <CheckCircle2 size={13} aria-hidden="true" /> {statusMessage}
              </>
            )}
          </span>
          <button
            className="publish"
            onClick={handlePublish}
            aria-label="Publish bot"
            title={publishTitle}
            disabled={!canPublish(role) || !draft}
          >
            <Send size={16} aria-hidden="true" /> Publish
          </button>
          {me && (
            <span className="user-chip" title={me.email || me.subject}>
              {me.platform_admin && <ShieldCheck size={14} aria-label="Platform admin" role="img" />}
              {me.subject}
            </span>
          )}
          <button className="icon-button" aria-label="Sign out" title="Sign out" onClick={onSignOut}>
            <LogOut size={17} aria-hidden="true" />
          </button>
        </div>
      </header>

      <aside className="sidebar">
        <nav aria-label="Studio">
          {workspace && (
            <>
              {tab("designer", "Designer", Bot)}
              {tab("versions", "Versions", GitBranch)}
              {tab("debugger", "Debugger", CircleHelp)}
              {tab("members", "Members", Users)}
            </>
          )}
          {tab("privacy", "Privacy", Lock)}
          {me?.platform_admin && tab("admin", "Admin", ShieldCheck)}
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
            <Plus size={17} aria-hidden="true" /> New dialog
          </button>
        )}
        <div className="sidebar-footer">
          Workspace
          <br />
          <strong>{workspace?.name ?? "—"}</strong>
        </div>
      </aside>

      <main id="main-content" className={`studio-main${showInspector ? "" : " full-width"}`} tabIndex={-1}>
      {activeTab === "admin" && me?.platform_admin ? (
        <AdminView api={api} onError={fail} onStatus={showStatus} />
      ) : activeTab === "privacy" ? (
        <PrivacyView api={api} workspace={workspace} onError={fail} onStatus={showStatus} />
      ) : activeTab === "members" && workspace ? (
        <MembersView api={api} workspace={workspace} onError={fail} onStatus={showStatus} />
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
                <Plus size={16} aria-hidden="true" /> New bot
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
                <Save size={18} aria-hidden="true" />
              </button>
              <button
                className="test-button"
                onClick={() => {
                  setTestOpen(true);
                  onTrack("test_chat_opened");
                }}
              >
                <Play size={16} aria-hidden="true" /> Test
              </button>
            </div>
          </div>
          <div className="canvas" role="group" aria-label="Conversation flow canvas">
            <div className="connector line-one" aria-hidden="true" />
            <div className="connector line-two" aria-hidden="true" />
            <div className="connector line-three" aria-hidden="true" />
            {nodes.map((node, index) => (
              <button
                className={`flow-node ${node.type.toLowerCase()} ${selectedNodeId === node.id ? "selected" : ""} node-${index}`}
                key={node.id}
                aria-pressed={selectedNodeId === node.id}
                onClick={() => setSelectedNodeId(node.id)}
              >
                <span className="node-type">{node.type}</span>
                <strong>{node.title}</strong>
                <small>{node.detail}</small>
              </button>
            ))}
            {editable && (
              <button className="add-node" aria-label="Add node" title="Add node" onClick={handleAddNode}>
                <Plus size={19} aria-hidden="true" />
              </button>
            )}
          </div>

          {testOpen && (
            <div ref={testDrawer} className="test-drawer" role="dialog" aria-modal="true" aria-labelledby="test-chat-title">
              <div className="test-header">
                <h2 id="test-chat-title">Interactive Test Chat</h2>
                <button className="icon-button" aria-label="Close test chat" onClick={closeTest}>
                  <X size={18} aria-hidden="true" />
                </button>
              </div>
              <p className="test-note">Simulated replies: the test chat is not connected to the conversation engine yet.</p>
              <div className="test-messages" role="log" aria-live="polite" aria-label="Test chat messages">
                {testMessages.map((m, idx) => (
                  <div key={idx} className={`test-msg ${m.role}`}>
                    <div>
                      <span className="sr-only">{m.role === "user" ? "You: " : "Bot: "}</span>
                      {m.text}
                    </div>
                    {m.options && (
                      <div>
                        {m.options.map((opt) => (
                          <button key={opt} type="button" className="test-menu-btn" onClick={() => handleSendTestMessage(opt)}>
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
                <input value={testInput} onChange={(e) => setTestInput(e.target.value)} placeholder="Type test input…" aria-label="Test message" />
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
          <form
            className="inline-form"
            aria-labelledby="audience-title"
            onSubmit={(e) => {
              e.preventDefault();
              handleSaveAudience();
            }}
          >
            <h3 id="audience-title" className="wide">
              Who is this bot for?
            </h3>
            <fieldset className="inspector-fields inline-fields" disabled={!editable}>
              <label>
                Audience
                <select value={audience} onChange={(e) => setAudience(e.target.value as Audience)} aria-describedby="audience-help">
                  <option value="general">General public</option>
                  <option value="minors">Children (needs a parent's or guardian's permission)</option>
                </select>
              </label>
              <label>
                Minimum age (0 for none)
                <input type="number" min={0} max={21} inputMode="numeric" value={minAge} onChange={(e) => setMinAge(e.target.value)} />
              </label>
              <button className="test-button" type="submit">
                Save
              </button>
            </fieldset>
            <p id="audience-help" className="field-help wide">
              With a minimum age, the chat asks people to confirm their age before they start. For children, it asks a parent or guardian
              instead, and publishing needs your confirmation that a parental-consent process is in place.
            </p>
          </form>
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
                      <strong className="active-pointer">Active Pointer</strong>
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
          <p className="admin-note">
            Traces will record live turn transitions once the engine Kafka consumer group is active.
          </p>
        </section>
      )}

      </main>

      {showInspector && (
        <aside className="inspector">
          <div className="inspector-title">
            <div>
              <p className="eyebrow">Selected node</p>
              <h2>{selected.type}</h2>
            </div>
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
              <Trash2 size={14} aria-hidden="true" style={{ marginRight: 6, verticalAlign: "middle" }} />
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
            <X size={14} aria-hidden="true" />
          </button>
        </div>
      )}

      {confirmMinors && (
        <Dialog
          alert
          title="Publish a bot for children?"
          onClose={() => setConfirmMinors(false)}
          description={
            <p>
              The chat will ask a parent or guardian for permission before a child can use it. The law may require more than that, such as
              checking who the parent is. Your confirmation is recorded in the audit log.
            </p>
          }
        >
          <label className="check-label dialog-field">
            <input type="checkbox" checked={guardianConfirmed} onChange={(e) => setGuardianConfirmed(e.target.checked)} />
            <span>I confirm that this area has an approved process for getting a parent's or guardian's consent.</span>
          </label>
          <div className="dialog-actions">
            <button type="button" className="choice-button" disabled={!guardianConfirmed} onClick={() => publish(true)}>
              Publish
            </button>
            <button type="button" className="choice-button" onClick={() => setConfirmMinors(false)}>
              Cancel
            </button>
          </div>
        </Dialog>
      )}
    </div>
  );
}
