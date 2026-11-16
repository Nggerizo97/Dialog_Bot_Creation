import { LitElement, css, html, nothing, unsafeCSS, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { THEME } from "./theme";

// Everything the widget keeps in the browser. All of it is sessionStorage: it lasts
// until the tab closes, and "Delete my data" and "End chat" remove it sooner.
// docs/legal/COOKIES_AND_STORAGE.md lists these keys; update it when this changes.
export const STORAGE_KEYS = {
  /** Random conversation ID. Strictly necessary. */
  user: "bot_dialog_generator_chat_user_id",
  /** The visible conversation, so it survives a reload. Strictly necessary. */
  messagesPrefix: "bot_dialog_generator_messages_",
  /** What the person accepted and opted into. Strictly necessary (it is the record of the choice). */
  consent: "bot_dialog_generator_consent",
} as const;

// Storage can be unavailable (private browsing, blocked site data); the chat then
// simply lasts until the page reloads.
function storageGet(key: string): string | null {
  try {
    return sessionStorage.getItem(key);
  } catch {
    return null;
  }
}

function storageSet(key: string, value: string): void {
  try {
    sessionStorage.setItem(key, value);
  } catch {
    // Keep the conversation in memory only.
  }
}

function storageRemove(key: string): void {
  try {
    sessionStorage.removeItem(key);
  } catch {
    // Nothing was stored.
  }
}

// Earlier versions kept conversations in localStorage with no expiry. Remove them.
function removeLegacyStorage(): void {
  try {
    for (const key of Object.keys(localStorage)) {
      if (key === STORAGE_KEYS.user || key.startsWith(STORAGE_KEYS.messagesPrefix)) localStorage.removeItem(key);
    }
  } catch {
    // Nothing to clean up.
  }
}

/** A conversation ID that cannot be guessed and says nothing about the person. */
function newUserId(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return "usr-" + Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

interface ChatMessage {
  /** "notice" and "error" come from the widget itself, not from the assistant. */
  role: "bot" | "user" | "notice" | "error";
  text: string;
  options?: Array<{ id: string; label: string }>;
}

/** Optional purposes. Each stays off until the person switches it on. */
export type OptionalPurpose = "analytics" | "campaigns";

interface ConsentState {
  /** The pre-chat step was completed (or there was nothing to accept). */
  accepted: boolean;
  processing: boolean;
  ageConfirmed: boolean;
  guardianConsent: boolean;
  guardianReference: string;
  /** null means the person has not been asked or has not answered yet. */
  analytics: boolean | null;
  campaigns: boolean | null;
}

const noConsent = (): ConsentState => ({
  accepted: false,
  processing: false,
  ageConfirmed: false,
  guardianConsent: false,
  guardianReference: "",
  analytics: null,
  campaigns: null,
});

const OPTIONAL_COPY: Record<OptionalPurpose, { label: string; short: string }> = {
  analytics: {
    label: "Share anonymous usage statistics, such as how many messages a chat has. What I write is never included.",
    short: "anonymous usage statistics",
  },
  campaigns: {
    label: "Remember which campaign or link brought me to this page.",
    short: "which campaign or link brought you here",
  },
};

const CAMPAIGN_KEYS = ["utm_source", "utm_medium", "utm_campaign"];
const CODE_VALUE = /^[A-Za-z0-9_.:-]{1,64}$/;

// Replies used only with the `demo` attribute, when no gateway is running. They say
// plainly that the data is a sample and that nobody is waiting on the other side.
const DEMO_BALANCE = "Sample data: a checking balance of $1,250.50 USD. This demo is not connected to any real account.";
const DEMO_HANDOFF =
  "This demo has no contact center, so no agent will join the chat. A live bot would hand the conversation to a person here.";

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])';

const themeVariables = unsafeCSS(
  Object.entries(THEME)
    .map(([name, value]) => `--bdg-${name.replace(/[A-Z]/g, (c) => "-" + c.toLowerCase())}: ${value};`)
    .join(" "),
);

@customElement("bot-dialog-generator-chat")
export class BotDialogGeneratorChat extends LitElement {
  @property() title = "Bot_Dialog_Generator assistant";
  @property({ attribute: "welcome-message" }) welcomeMessage = "Hi, how can we help today?";
  @property({ attribute: "gateway-url" }) gatewayUrl = "http://localhost:8081";
  @property({ attribute: "tenant" }) tenant = "demo";
  /** Links to the published legal documents. Each is shown only when set. */
  @property({ attribute: "privacy-url" }) privacyUrl = "";
  @property({ attribute: "terms-url" }) termsUrl = "";
  @property({ attribute: "cookies-url" }) cookiesUrl = "";
  /** Version of the privacy policy the person accepts, kept with the proof of consent. */
  @property({ attribute: "policy-version" }) policyVersion = "";
  /** Ask for explicit authorization before the first message (for example, Colombia's Ley 1581). */
  @property({ attribute: "require-authorization", type: Boolean }) requireAuthorization = false;
  /** Minimum age to use the chat. 0 means no age question. */
  @property({ attribute: "min-age", type: Number }) minAge = 0;
  /** "minors" when the bot is for children: a parent or guardian must agree first. */
  @property() audience: "general" | "minors" = "general";
  /**
   * How parental consent is given. "checkbox": the guardian ticks a box in the chat.
   * "host": the page runs its own verified process and calls grantParentalConsent().
   */
  @property({ attribute: "parental-consent" }) parentalConsent: "checkbox" | "host" = "checkbox";
  /** Address of the analytics service. When set, the person is asked to opt in; nothing is sent before that. */
  @property({ attribute: "analytics-url" }) analyticsUrl = "";
  /** Offer the "campaigns" opt-in: remembering which campaign link brought the person here. */
  @property({ attribute: "campaign-attribution", type: Boolean }) campaignAttribution = false;
  /** Answer with built-in sample replies when the gateway cannot be reached. For previews only. */
  @property({ type: Boolean }) demo = false;
  /** Include what the person typed in the `bot-dialog-generator-message` event. Off by default. */
  @property({ attribute: "expose-message-text", type: Boolean }) exposeMessageText = false;

  @state() private open = false;
  @state() private messages: ChatMessage[] = [];
  @state() private loading = false;
  @state() private view: "chat" | "choices" | "delete" = "chat";
  @state() private menuOpen = false;
  @state() private consent: ConsentState = noConsent();
  @state() private formError = "";
  @state() private busy = false;
  private userId: string;

  constructor() {
    super();
    // The conversation lives only as long as the browser tab (sessionStorage): it
    // survives page reloads, and it is gone when the tab closes, so the next person on
    // a shared computer cannot read it.
    removeLegacyStorage();
    const storedUser = storageGet(STORAGE_KEYS.user);
    if (storedUser) {
      this.userId = storedUser;
    } else {
      this.userId = newUserId();
      storageSet(STORAGE_KEYS.user, this.userId);
    }
  }

  connectedCallback() {
    super.connectedCallback();
    this.restoreSession();
  }

  private restoreSession() {
    const savedConsent = storageGet(STORAGE_KEYS.consent);
    if (savedConsent) {
      try {
        this.consent = { ...noConsent(), ...JSON.parse(savedConsent) };
      } catch {
        // Ask again.
      }
    }
    const saved = storageGet(`${STORAGE_KEYS.messagesPrefix}${this.userId}`);
    if (saved) {
      try {
        this.messages = JSON.parse(saved);
        return;
      } catch {
        // Fallback to fresh session
      }
    }
    this.messages = [{ role: "bot", text: this.welcomeMessage }];
  }

  private saveSession() {
    storageSet(STORAGE_KEYS.user, this.userId);
    storageSet(`${STORAGE_KEYS.messagesPrefix}${this.userId}`, JSON.stringify(this.messages));
  }

  private saveConsent() {
    storageSet(STORAGE_KEYS.consent, JSON.stringify(this.consent));
  }

  /** Removes everything this widget keeps in the browser and starts over. */
  private forgetLocally() {
    storageRemove(`${STORAGE_KEYS.messagesPrefix}${this.userId}`);
    storageRemove(STORAGE_KEYS.user);
    storageRemove(STORAGE_KEYS.consent);
    this.userId = newUserId();
    this.consent = noConsent();
    this.messages = [{ role: "bot", text: this.welcomeMessage }];
  }

  // --- What needs the person's agreement -------------------------------------------

  private get optionalPurposes(): OptionalPurpose[] {
    const purposes: OptionalPurpose[] = [];
    if (this.analyticsUrl) purposes.push("analytics");
    if (this.campaignAttribution) purposes.push("campaigns");
    return purposes;
  }

  private get needsGuardian(): boolean {
    return this.audience === "minors";
  }

  /** The chat cannot start until the required boxes are ticked. */
  private get gateOpen(): boolean {
    const required = this.requireAuthorization || this.minAge > 0 || this.needsGuardian;
    return required && !this.consent.accepted;
  }

  /** Optional purposes nobody has answered yet: asked without blocking the chat. */
  private get unanswered(): OptionalPurpose[] {
    return this.gateOpen ? [] : this.optionalPurposes.filter((p) => this.consent[p] === null);
  }

  private granted(purpose: OptionalPurpose): boolean {
    return this.optionalPurposes.includes(purpose) && this.consent[purpose] === true;
  }

  /**
   * Called by the page when its own parental-consent process has verified a parent or
   * guardian (`parental-consent="host"`). `reference` identifies that verification in
   * the page's records and is kept with the proof of consent.
   */
  grantParentalConsent(reference: string) {
    this.consent = { ...this.consent, guardianConsent: true, guardianReference: String(reference).slice(0, 256) };
    this.saveConsent();
  }

  /** Sends the proof of consent to the gateway. Reports whether it was recorded. */
  private async recordConsent(): Promise<boolean> {
    const purposes: Record<string, boolean> = {};
    if (this.requireAuthorization) purposes.processing = this.consent.processing;
    for (const p of this.optionalPurposes) purposes[p] = this.consent[p] === true;
    try {
      const res = await fetch(`${this.gatewayUrl}/channels/webchat/consent`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          user_id: this.userId,
          purposes,
          policy_version: this.policyVersion,
          age_confirmed: this.consent.ageConfirmed,
          guardian_consent: this.consent.guardianConsent,
          guardian_reference: this.consent.guardianReference,
        }),
      });
      return res.ok;
    } catch {
      return false;
    }
  }

  private announceConsent() {
    this.dispatchEvent(
      new CustomEvent("bot-dialog-generator-consent", {
        detail: { analytics: this.granted("analytics"), campaigns: this.granted("campaigns") },
        bubbles: true,
        composed: true,
      }),
    );
  }

  private acceptGate = async (event: SubmitEvent) => {
    event.preventDefault();
    const form = event.currentTarget as HTMLFormElement;
    const ticked = (name: string) => (form.elements.namedItem(name) as HTMLInputElement | null)?.checked === true;

    const missing: string[] = [];
    if (this.requireAuthorization && !ticked("processing")) missing.push("processing");
    if (this.minAge > 0 && !this.needsGuardian && !ticked("age")) missing.push("age");
    const guardianByHost = this.needsGuardian && this.parentalConsent === "host";
    if (this.needsGuardian && !guardianByHost && !ticked("guardian")) missing.push("guardian");
    if (guardianByHost && !this.consent.guardianConsent) {
      this.formError = "A parent or guardian needs to give permission on this page before the chat can start.";
      return;
    }
    if (missing.length > 0) {
      this.formError = "Tick the boxes marked as required to start the chat.";
      (form.elements.namedItem(missing[0]) as HTMLInputElement).focus();
      return;
    }

    this.busy = true;
    this.formError = "";
    this.consent = {
      ...this.consent,
      accepted: true,
      processing: this.requireAuthorization,
      ageConfirmed: this.minAge > 0 && !this.needsGuardian,
      guardianConsent: this.needsGuardian,
      analytics: this.optionalPurposes.includes("analytics") ? ticked("analytics") : null,
      campaigns: this.optionalPurposes.includes("campaigns") ? ticked("campaigns") : null,
    };
    const recorded = await this.recordConsent();
    this.busy = false;
    if (!recorded && !this.demo) {
      // Without the proof, the chat does not start: an authorization nobody can show was never given.
      this.consent = { ...this.consent, accepted: false };
      this.formError = "We could not save your choice. Check your connection and try again.";
      return;
    }
    this.saveConsent();
    this.announceConsent();
    this.track("chat_opened");
    await this.updateComplete;
    this.focusInitial();
  };

  /** "Allow" or "No thanks" on the optional prompt: one click either way. */
  private answerOptional(allow: boolean) {
    const next = { ...this.consent };
    for (const p of this.unanswered) next[p] = allow;
    this.applyOptional(next);
  }

  private saveChoices = (event: SubmitEvent) => {
    event.preventDefault();
    const form = event.currentTarget as HTMLFormElement;
    const next = { ...this.consent };
    for (const p of this.optionalPurposes) next[p] = (form.elements.namedItem(p) as HTMLInputElement).checked;
    this.applyOptional(next);
    this.view = "chat";
  };

  private applyOptional(next: ConsentState) {
    this.consent = next;
    this.saveConsent();
    this.announceConsent();
    void this.recordConsent();
  }

  // --- Analytics and campaign attribution: nothing without the opt-in ---------------

  private campaignContext(): Record<string, string> | undefined {
    if (!this.granted("campaigns")) return undefined;
    const params = new URLSearchParams(window.location.search);
    const context: Record<string, string> = {};
    for (const key of CAMPAIGN_KEYS) {
      const value = params.get(key);
      if (value && CODE_VALUE.test(value)) context[key] = value;
    }
    return Object.keys(context).length > 0 ? context : undefined;
  }

  /** Counts an event. Never includes what the person wrote or their conversation ID. */
  private track(event: "chat_opened" | "message_sent" | "option_selected" | "chat_closed") {
    if (!this.granted("analytics")) return;
    const body = JSON.stringify({
      source: "webchat",
      event,
      consent: { analytics: true, campaigns: this.granted("campaigns") },
      props: { channel: "webchat", ...this.campaignContext() },
    });
    fetch(`${this.analyticsUrl.replace(/\/$/, "")}/events`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body,
      keepalive: true,
    }).catch(() => {
      // Statistics are optional; the chat does not depend on them.
    });
  }

  static styles = css`
    :host { ${themeVariables} color: var(--bdg-ink); font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
    button, input { font: inherit; } button { cursor: pointer; }
    h2 { margin: 0; font-size: 15px; }
    p { margin: 0; }
    a { color: var(--bdg-accent-text); text-decoration: underline; }
    .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
    button:focus-visible, input:focus-visible, a:focus-visible { outline: 2px solid var(--bdg-focus-on-light); outline-offset: 2px; }

    .launcher { position: fixed; right: 24px; bottom: 24px; z-index: 999; width: 56px; height: 56px; color: var(--bdg-on-accent); background: var(--bdg-accent); border: 0; border-radius: 50%; box-shadow: 0 8px 24px rgba(24, 32, 31, 0.25); display: grid; place-items: center; transition: transform 0.2s; }
    .launcher:hover { transform: scale(1.06); }
    /* The page behind the launcher can be any color: a light ring inside a dark one shows on both. */
    .launcher:focus-visible { outline: 3px solid var(--bdg-on-accent); outline-offset: 0; box-shadow: 0 0 0 6px var(--bdg-ink); }
    .launcher svg { width: 26px; height: 26px; }

    .panel { position: fixed; right: 24px; bottom: 92px; z-index: 999; display: flex; flex-direction: column; width: min(380px, calc(100vw - 32px)); height: min(560px, calc(100vh - 112px)); overflow: hidden; background: var(--bdg-surface); border: 1px solid var(--bdg-control-border); border-radius: 10px; box-shadow: 0 16px 40px rgba(24, 32, 31, 0.2); }
    header { position: relative; display: flex; align-items: center; gap: 4px; padding: 10px 10px 10px 18px; color: var(--bdg-on-header); background: var(--bdg-header); }
    header h2 { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 700; }
    .icon { display: grid; place-items: center; width: 36px; height: 36px; color: inherit; background: transparent; border: 1px solid transparent; border-radius: 6px; font-size: 20px; line-height: 1; }
    .icon:hover { border-color: var(--bdg-on-header); }
    header :focus-visible { outline-color: var(--bdg-focus-on-dark); }

    .menu { position: absolute; top: 100%; right: 10px; z-index: 2; display: grid; min-width: 220px; padding: 6px; color: var(--bdg-ink); background: var(--bdg-surface); border: 1px solid var(--bdg-control-border); border-radius: 8px; box-shadow: 0 10px 24px rgba(24, 32, 31, 0.2); }
    /* Every entry looks the same: leaving or deleting is as easy to find as anything else. */
    .menu a, .menu button { display: block; padding: 9px 10px; color: var(--bdg-ink); text-align: left; text-decoration: none; background: transparent; border: 0; border-radius: 5px; font-size: 13px; font-weight: 600; }
    .menu a:hover, .menu button:hover { background: var(--bdg-accent-tint); }
    .menu :focus-visible { outline-color: var(--bdg-focus-on-light); outline-offset: 0; }

    .disclosure { padding: 6px 18px; color: var(--bdg-ink-muted); background: var(--bdg-surface-note); font-size: 12px; line-height: 1.4; }
    .body { display: flex; flex: 1; min-height: 0; flex-direction: column; }
    .messages { display: grid; flex: 1; align-content: start; gap: 12px; padding: 16px; overflow-y: auto; background: var(--bdg-surface-muted); }
    .message { max-width: 82%; padding: 10px 14px; background: var(--bdg-surface); border: 1px solid var(--bdg-divider); border-radius: 8px; font-size: 13px; line-height: 1.45; overflow-wrap: anywhere; }
    .message.user { justify-self: end; color: var(--bdg-on-accent); background: var(--bdg-accent); border-color: var(--bdg-accent); }
    .message.notice { max-width: none; color: var(--bdg-ink); background: transparent; border-style: dashed; border-color: var(--bdg-control-border); }
    .message.error { max-width: none; color: var(--bdg-danger); background: var(--bdg-danger-tint); border-color: var(--bdg-danger); }
    .menu-options { display: grid; gap: 6px; margin-top: 8px; }
    .menu-btn { padding: 7px 10px; color: var(--bdg-accent-text); background: var(--bdg-accent-tint); border: 1px solid var(--bdg-control-border); border-radius: 5px; font-size: 13px; font-weight: 600; text-align: left; }
    .menu-btn:hover { background: var(--bdg-accent-tint-hover); }
    .loading-indicator { font-size: 12px; color: var(--bdg-ink-muted); font-style: italic; padding: 4px 8px; }

    .prompt { display: grid; gap: 8px; padding: 10px 12px; border-top: 1px solid var(--bdg-divider); background: var(--bdg-surface); font-size: 12px; line-height: 1.45; }
    .choices { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
    /* Both answers share one style, on purpose: accepting is never made easier than declining. */
    .choice { padding: 9px 10px; color: var(--bdg-accent-text); background: var(--bdg-surface); border: 1px solid var(--bdg-control-border); border-radius: 5px; font-size: 13px; font-weight: 700; }
    .choice:hover { background: var(--bdg-accent-tint); }
    .choice:disabled { opacity: 0.6; cursor: progress; }

    .card { display: grid; flex: 1; align-content: start; gap: 12px; padding: 18px; overflow-y: auto; font-size: 13px; line-height: 1.5; }
    .card fieldset { display: grid; gap: 10px; margin: 0; padding: 0; border: 0; }
    .card legend { padding: 0; margin-bottom: 4px; font-weight: 700; }
    .check { display: grid; grid-template-columns: auto 1fr; gap: 10px; align-items: start; }
    .check input { width: 18px; height: 18px; margin: 1px 0 0; accent-color: var(--bdg-accent); }
    .hint { color: var(--bdg-ink-muted); font-size: 12px; }
    .form-error { color: var(--bdg-danger); font-weight: 600; }

    .composer { display: flex; gap: 8px; padding: 12px; border-top: 1px solid var(--bdg-divider); background: var(--bdg-surface); }
    .composer input { min-width: 0; flex: 1; padding: 9px 12px; color: var(--bdg-ink); background: var(--bdg-surface); border: 1px solid var(--bdg-control-border); border-radius: 5px; font-size: 13px; }
    .composer input::placeholder { color: var(--bdg-ink-muted); opacity: 1; }
    .composer button { padding: 9px 14px; color: var(--bdg-on-accent); background: var(--bdg-accent); border: 0; border-radius: 5px; font-weight: 700; font-size: 13px; }
    footer { display: flex; flex-wrap: wrap; gap: 4px 14px; padding: 6px 12px 10px; background: var(--bdg-surface); font-size: 12px; }
    footer a { color: var(--bdg-ink-muted); }

    @media (prefers-reduced-motion: reduce) { .launcher { transition: none; } .launcher:hover { transform: none; } }
    @media (forced-colors: active) { .launcher, .panel, .choice, .menu-btn, .message { border: 1px solid CanvasText; } }
  `;

  private async sendMessage(text: string, choice?: string) {
    if (!text && !choice) return;
    if (this.gateOpen) return;

    const userEntry: ChatMessage = {
      role: "user",
      text: choice ? `Option: ${text}` : text,
    };
    this.messages = [...this.messages, userEntry];
    this.saveSession();
    this.loading = true;
    this.track(choice ? "option_selected" : "message_sent");

    // The page learns that a message was sent, not what it said: scripts on the page
    // (analytics tags, session recorders) would otherwise receive every word.
    const detail: Record<string, string | undefined> = { kind: choice ? "choice" : "text", choice };
    if (this.exposeMessageText) detail.text = text;
    this.dispatchEvent(new CustomEvent("bot-dialog-generator-message", { detail, bubbles: true, composed: true }));

    try {
      const payload: Record<string, unknown> = {
        tenant: this.tenant,
        channel: "webchat",
        user_id: this.userId,
        consent: { analytics: this.granted("analytics"), campaigns: this.granted("campaigns") },
      };
      if (choice) {
        payload.choice = choice;
      } else {
        payload.text = text;
      }
      const context = this.campaignContext();
      if (context) payload.context = context;

      const res = await fetch(`${this.gatewayUrl}/channels/webchat/messages`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });

      if (res.ok) {
        const data = await res.json();
        const incoming: ChatMessage[] = [];
        if (data.messages && Array.isArray(data.messages)) {
          for (const m of data.messages) {
            if (m.kind === "text") {
              incoming.push({ role: "bot", text: m.text });
            } else if (m.kind === "menu") {
              incoming.push({
                role: "bot",
                text: m.prompt || "Please select:",
                options: m.options,
              });
            }
          }
        }
        if (incoming.length > 0) {
          this.messages = [...this.messages, ...incoming];
          this.saveSession();
          this.loading = false;
          return;
        }
      }
    } catch {
      // Handled below: the gateway could not be reached.
    }

    if (!this.demo) {
      // Never make up an answer: say that the assistant could not be reached.
      this.loading = false;
      this.messages = [
        ...this.messages,
        { role: "error", text: "We could not reach the assistant. Check your connection and send your message again." },
      ];
      this.saveSession();
      return;
    }

    // Sample replies for the `demo` attribute.
    setTimeout(() => {
      this.loading = false;
      const lower = text.toLowerCase();
      if (lower.includes("balance") || choice === "balance") {
        this.messages = [...this.messages, { role: "bot", text: DEMO_BALANCE }];
      } else if (choice === "contact_center" || lower.includes("person") || lower.includes("agent")) {
        this.messages = [...this.messages, { role: "bot", text: DEMO_HANDOFF }];
      } else if (choice === "ai_assistant") {
        this.messages = [
          ...this.messages,
          { role: "bot", text: "You're chatting with the AI assistant. AI answers are not connected in this demo yet." },
          {
            role: "bot",
            text: "You can talk to a person at any time.",
            options: [
              { id: "contact_center", label: "Talk to a person" },
              { id: "main_menu", label: "Back to main menu" },
            ],
          },
        ];
      } else if (lower.includes("advisor") || choice === "handoff") {
        // Ask before transferring: AI assistant first, or a person from the contact center.
        this.messages = [
          ...this.messages,
          {
            role: "bot",
            text: "Would you like to chat with our AI assistant or talk to a person from the contact center?",
            options: [
              { id: "ai_assistant", label: "AI assistant" },
              { id: "contact_center", label: "Contact center agent" },
            ],
          },
        ];
      } else {
        this.messages = [
          ...this.messages,
          {
            role: "bot",
            text: "Welcome to the Bot_Dialog_Generator demo!",
            options: [
              { id: "balance", label: "Check balance" },
              { id: "handoff", label: "Connect to advisor" },
            ],
          },
        ];
      }
      this.saveSession();
    }, 300);
  }

  private send(event: SubmitEvent) {
    event.preventDefault();
    const form = event.currentTarget as HTMLFormElement;
    const input = form.elements.namedItem("message") as HTMLInputElement;
    const value = input.value.trim();
    if (!value) return;
    input.value = "";
    this.sendMessage(value);
  }

  // --- Leaving, and deleting ---------------------------------------------------------

  private openPanel = () => {
    this.open = true;
    if (!this.gateOpen) this.track("chat_opened");
  };

  private close = async () => {
    this.track("chat_closed");
    this.open = false;
    this.menuOpen = false;
    this.view = "chat";
    this.formError = "";
    await this.updateComplete;
    this.renderRoot.querySelector<HTMLButtonElement>(".launcher")?.focus();
  };

  /** Removes the conversation from this browser and closes the chat. */
  private endChat = async () => {
    this.forgetLocally();
    await this.close();
  };

  /**
   * Deletes the conversation here and asks the gateway to delete what the servers
   * hold about it. If the servers cannot be reached, nothing is deleted and the person
   * is told so: a deletion we cannot confirm is not reported as done.
   */
  async deleteMyData(): Promise<boolean> {
    this.busy = true;
    this.formError = "";
    let reference = "";
    let reached = false;
    try {
      const res = await fetch(`${this.gatewayUrl}/channels/webchat/data-deletion`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ user_id: this.userId }),
      });
      if (res.ok) {
        reached = true;
        reference = String((await res.json()).reference ?? "");
      }
    } catch {
      // Handled below.
    }
    this.busy = false;
    if (!reached && !this.demo) {
      this.formError = "We could not reach our servers, so nothing was deleted yet. Check your connection and try again.";
      return false;
    }
    this.forgetLocally();
    const notice = reached
      ? `Your data was deleted: this conversation in your browser, and what our servers held about it.${reference ? ` Reference: ${reference}.` : ""}`
      : "This demo keeps nothing on a server. Your conversation was deleted from this browser.";
    this.messages = [{ role: "notice", text: notice }, ...this.messages];
    this.view = "chat";
    this.dispatchEvent(new CustomEvent("bot-dialog-generator-data-deleted", { detail: { reference }, bubbles: true, composed: true }));
    await this.updateComplete;
    this.focusInitial();
    return true;
  }

  // --- Keyboard and focus --------------------------------------------------------------

  private get panel(): HTMLElement | null {
    return this.renderRoot.querySelector<HTMLElement>(".panel");
  }

  private get activeElement(): Element | null {
    return (this.renderRoot as ShadowRoot).activeElement;
  }

  /** Moves focus to where the person will act next. */
  private focusInitial() {
    const root = this.renderRoot;
    const target =
      root.querySelector<HTMLElement>(".card input, .card button") ??
      root.querySelector<HTMLElement>('input[name="message"]') ??
      root.querySelector<HTMLElement>(".panel button");
    target?.focus();
  }

  private toggleMenu = () => {
    this.menuOpen = !this.menuOpen;
  };

  private async closeMenu(restoreFocus = true) {
    this.menuOpen = false;
    await this.updateComplete;
    if (restoreFocus) this.renderRoot.querySelector<HTMLElement>(".menu-toggle")?.focus();
  }

  private showView(view: "choices" | "delete") {
    this.menuOpen = false;
    this.formError = "";
    this.view = view;
  }

  private backToChat = () => {
    this.formError = "";
    this.view = "chat";
  };

  protected updated(changed: PropertyValues) {
    const opened = changed.has("open") && this.open;
    const viewChanged = changed.has("view") && this.open;
    if (opened || viewChanged) this.focusInitial();
    if (changed.has("menuOpen") && this.menuOpen) {
      this.renderRoot.querySelector<HTMLElement>(".menu [role=menuitem]")?.focus();
    }
    if (changed.has("messages")) {
      const log = this.renderRoot.querySelector<HTMLElement>(".messages");
      if (log) log.scrollTop = log.scrollHeight;
    }
  }

  private onKeydown = (event: KeyboardEvent) => {
    if (event.key === "Escape") {
      event.stopPropagation();
      if (this.menuOpen) void this.closeMenu();
      else if (this.view !== "chat") this.backToChat();
      else void this.close();
      return;
    }
    if (this.menuOpen && (event.key === "ArrowDown" || event.key === "ArrowUp" || event.key === "Home" || event.key === "End")) {
      const items = [...this.renderRoot.querySelectorAll<HTMLElement>(".menu [role=menuitem]")];
      const current = items.indexOf(this.activeElement as HTMLElement);
      const next =
        event.key === "Home" ? 0 : event.key === "End" ? items.length - 1 : (current + (event.key === "ArrowDown" ? 1 : -1) + items.length) % items.length;
      items[next]?.focus();
      event.preventDefault();
      return;
    }
    if (event.key !== "Tab" || !this.panel) return;
    // Keep Tab inside the open chat; Escape or the close button leaves it.
    const focusable = [...this.panel.querySelectorAll<HTMLElement>(FOCUSABLE)];
    if (focusable.length === 0) return;
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    const active = this.activeElement;
    if (event.shiftKey && (active === first || !active)) {
      last.focus();
      event.preventDefault();
    } else if (!event.shiftKey && active === last) {
      first.focus();
      event.preventDefault();
    }
  };

  // --- Rendering -------------------------------------------------------------------------

  private link(url: string, label: string, role?: "menuitem") {
    if (!url) return nothing;
    return html`<a href=${url} target="_blank" rel="noopener noreferrer" role=${role ?? nothing}>${label}<span class="sr-only"> (opens in a new tab)</span></a>`;
  }

  private policyLinks() {
    if (!this.privacyUrl && !this.termsUrl) return nothing;
    return html`<p>
      Read the ${this.link(this.privacyUrl, "Privacy Policy")}${this.privacyUrl && this.termsUrl ? " and the " : ""}${this.link(this.termsUrl, "Terms of Use")}
      to see how your messages are used and how to delete them.
    </p>`;
  }

  private optionalBoxes(prefill: boolean) {
    if (this.optionalPurposes.length === 0) return nothing;
    return html`
      <fieldset>
        <legend>Optional</legend>
        ${this.optionalPurposes.map(
          (p) => html`
            <label class="check">
              <input type="checkbox" name=${p} .checked=${prefill && this.consent[p] === true} />
              <span>${OPTIONAL_COPY[p].label}</span>
            </label>
          `,
        )}
        <p class="hint">The chat works the same if you leave these off. You can change them later under “Privacy choices”.</p>
      </fieldset>
    `;
  }

  private renderGate() {
    const guardianByHost = this.needsGuardian && this.parentalConsent === "host";
    return html`
      <form class="card gate" novalidate aria-labelledby="gate-title" @submit=${this.acceptGate}>
        <h2 id="gate-title">Before we start</h2>
        <p>You are about to chat with an automated assistant, not a person.</p>
        ${this.policyLinks()}
        <fieldset>
          <legend>Required to use the chat</legend>
          ${this.requireAuthorization
            ? html`<label class="check">
                <input type="checkbox" name="processing" required aria-required="true" />
                <span>I agree that my messages are used to answer my request, as the Privacy Policy explains.</span>
              </label>`
            : nothing}
          ${this.minAge > 0 && !this.needsGuardian
            ? html`<label class="check">
                <input type="checkbox" name="age" required aria-required="true" />
                <span>I am ${this.minAge} or older.</span>
              </label>`
            : nothing}
          ${this.needsGuardian && !guardianByHost
            ? html`<label class="check">
                <input type="checkbox" name="guardian" required aria-required="true" />
                <span>
                  I am the parent or legal guardian of the child who will use this chat${this.minAge > 0 ? html` (under ${this.minAge})` : nothing},
                  and I agree to them using it.
                </span>
              </label>`
            : nothing}
          ${guardianByHost
            ? html`<p class="hint">
                ${this.consent.guardianConsent
                  ? "A parent or guardian has given permission."
                  : "This chat is for young people. A parent or guardian needs to give permission on this page first."}
              </p>`
            : nothing}
        </fieldset>
        ${this.optionalBoxes(false)}
        ${this.formError ? html`<p class="form-error" role="alert">${this.formError}</p>` : nothing}
        <div class="choices">
          <button class="choice" type="submit" ?disabled=${this.busy}>Start chat</button>
          <button class="choice" type="button" @click=${this.close}>Not now</button>
        </div>
      </form>
    `;
  }

  private renderChoices() {
    return html`
      <form class="card" aria-labelledby="choices-title" @submit=${this.saveChoices}>
        <h2 id="choices-title">Privacy choices</h2>
        <p>The chat keeps your conversation in this browser tab so it can work. Everything below is optional and off unless you turn it on.</p>
        ${this.optionalBoxes(true)}
        <div class="choices">
          <button class="choice" type="submit">Save choices</button>
          <button class="choice" type="button" @click=${this.backToChat}>Cancel</button>
        </div>
      </form>
    `;
  }

  private renderDelete() {
    return html`
      <div class="card" role="alertdialog" aria-labelledby="delete-title" aria-describedby="delete-text">
        <h2 id="delete-title">Delete my data?</h2>
        <p id="delete-text">
          This removes the conversation from this browser and deletes what our servers hold about it. It cannot be undone, and the chat starts
          again from the beginning.
        </p>
        ${this.formError ? html`<p class="form-error" role="alert">${this.formError}</p>` : nothing}
        <div class="choices">
          <button class="choice" type="button" ?disabled=${this.busy} @click=${() => this.deleteMyData()}>Delete my data</button>
          <button class="choice" type="button" @click=${this.backToChat}>Keep my data</button>
        </div>
      </div>
    `;
  }

  private renderMenu() {
    if (!this.menuOpen) return nothing;
    return html`
      <div class="menu" id="chat-menu" role="menu" aria-label="Chat options">
        ${this.link(this.privacyUrl, "Privacy Policy", "menuitem")} ${this.link(this.termsUrl, "Terms of Use", "menuitem")}
        ${this.link(this.cookiesUrl, "Cookies and storage", "menuitem")}
        ${this.optionalPurposes.length > 0 && !this.gateOpen
          ? html`<button type="button" role="menuitem" @click=${() => this.showView("choices")}>Privacy choices</button>`
          : nothing}
        <button type="button" role="menuitem" @click=${() => this.showView("delete")}>Delete my data</button>
        <button type="button" role="menuitem" @click=${this.endChat}>End chat and clear it</button>
      </div>
    `;
  }

  private renderConversation() {
    const unanswered = this.unanswered;
    return html`
      <div class="messages" role="log" aria-live="polite" aria-relevant="additions" aria-label="Conversation" tabindex="0">
        ${this.messages.map(
          (msg) => html`
            <div class="message ${msg.role === "bot" ? "" : msg.role}" role=${msg.role === "error" ? "alert" : nothing}>
              <div>
                <span class="sr-only">${msg.role === "user" ? "You: " : msg.role === "bot" ? "Assistant: " : ""}</span>${msg.text}
              </div>
              ${msg.options
                ? html`
                    <div class="menu-options">
                      ${msg.options.map(
                        (opt) => html`
                          <button class="menu-btn" type="button" @click=${() => this.sendMessage(opt.label, opt.id)}>${opt.label}</button>
                        `,
                      )}
                    </div>
                  `
                : nothing}
            </div>
          `,
        )}
        ${this.loading ? html`<div class="loading-indicator">The assistant is writing…</div>` : nothing}
      </div>
      ${unanswered.length > 0
        ? html`
            <div class="prompt" role="group" aria-label="Optional privacy choices">
              <p>
                Optional: may this chat keep ${unanswered.map((p) => OPTIONAL_COPY[p].short).join(", and ")}? Your messages are never
                included, and the chat works the same either way.
              </p>
              <div class="choices">
                <button class="choice" type="button" @click=${() => this.answerOptional(true)}>Allow</button>
                <button class="choice" type="button" @click=${() => this.answerOptional(false)}>No thanks</button>
              </div>
            </div>
          `
        : nothing}
      <form class="composer" @submit=${this.send}>
        <input name="message" aria-label="Message" placeholder="Type a message…" autocomplete="off" maxlength="4000" enterkeyhint="send" />
        <button type="submit">Send</button>
      </form>
    `;
  }

  render() {
    if (!this.open) {
      return html`<button class="launcher" type="button" aria-label="Open chat" aria-haspopup="dialog" @click=${this.openPanel}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
          <path d="M4 5h16v11H9l-5 4z" />
        </svg>
      </button>`;
    }
    const body = this.gateOpen ? this.renderGate() : this.view === "choices" ? this.renderChoices() : this.view === "delete" ? this.renderDelete() : this.renderConversation();
    const hasLinks = this.privacyUrl || this.termsUrl || this.cookiesUrl;
    return html`
      <section class="panel" role="dialog" aria-modal="true" aria-labelledby="chat-title" @keydown=${this.onKeydown}>
        <header>
          <h2 id="chat-title">${this.title}</h2>
          <button
            class="icon menu-toggle"
            type="button"
            aria-label="Chat options"
            aria-haspopup="menu"
            aria-expanded=${this.menuOpen ? "true" : "false"}
            aria-controls="chat-menu"
            @click=${this.toggleMenu}
          >
            <span aria-hidden="true">⋮</span>
          </button>
          <button class="icon" type="button" aria-label="Close chat" @click=${this.close}><span aria-hidden="true">×</span></button>
          ${this.renderMenu()}
        </header>
        <p class="disclosure">You're chatting with an automated assistant, not a person.</p>
        <div class="body">${body}</div>
        ${hasLinks
          ? html`<footer>
              ${this.link(this.privacyUrl, "Privacy Policy")} ${this.link(this.termsUrl, "Terms of Use")}
              ${this.link(this.cookiesUrl, "Cookies and storage")}
            </footer>`
          : nothing}
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "bot-dialog-generator-chat": BotDialogGeneratorChat;
  }
}
