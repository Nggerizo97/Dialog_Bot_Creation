import { LitElement, css, html } from "lit";
import { customElement, property, state } from "lit/decorators.js";

interface ChatMessage {
  role: "bot" | "user";
  text: string;
  options?: Array<{ id: string; label: string }>;
}

@customElement("bot-dialog-generator-chat")
export class BotDialogGeneratorChat extends LitElement {
  @property() title = "Bot_Dialog_Generator assistant";
  @property({ attribute: "welcome-message" }) welcomeMessage = "Hi, how can we help today?";
  @property({ attribute: "gateway-url" }) gatewayUrl = "http://localhost:8081";
  @property({ attribute: "tenant" }) tenant = "demo";

  @state() private open = false;
  @state() private messages: ChatMessage[] = [];
  @state() private loading = false;
  private userId: string;

  constructor() {
    super();
    // Maintain persistent session across page reloads
    const storedUser = localStorage.getItem("bot_dialog_generator_chat_user_id");
    if (storedUser) {
      this.userId = storedUser;
    } else {
      this.userId = "usr-" + Math.random().toString(36).substring(2, 9);
      localStorage.setItem("bot_dialog_generator_chat_user_id", this.userId);
    }
  }

  connectedCallback() {
    super.connectedCallback();
    this.restoreSession();
  }

  private restoreSession() {
    const saved = localStorage.getItem(`bot_dialog_generator_messages_${this.userId}`);
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
    localStorage.setItem(`bot_dialog_generator_messages_${this.userId}`, JSON.stringify(this.messages));
  }

  static styles = css`
    :host { color: #18201f; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
    button, input { font: inherit; } button { cursor: pointer; }
    .launcher { position: fixed; right: 24px; bottom: 24px; z-index: 999; width: 56px; height: 56px; color: #fff; background: #15766b; border: 0; border-radius: 50%; box-shadow: 0 8px 24px rgba(24, 32, 31, 0.25); font-size: 24px; display: grid; place-items: center; transition: transform 0.2s; }
    .launcher:hover { transform: scale(1.06); }
    .panel { position: fixed; right: 24px; bottom: 92px; z-index: 999; display: grid; grid-template-rows: auto 1fr auto; width: min(380px, calc(100vw - 32px)); height: 530px; overflow: hidden; background: #fffef9; border: 1px solid #cbd4c9; border-radius: 10px; box-shadow: 0 16px 40px rgba(24, 32, 31, 0.2); }
    header { display: flex; align-items: center; justify-content: space-between; padding: 14px 18px; color: #fffef9; background: #18201f; font-weight: 700; font-size: 15px; }
    header button { color: inherit; background: transparent; border: 0; font-size: 18px; line-height: 1; }
    .messages { display: grid; align-content: start; gap: 12px; padding: 16px; overflow-y: auto; background: #f0f4f0; }
    .message { max-width: 82%; padding: 10px 14px; background: #fffef9; border: 1px solid #d5ddd5; border-radius: 8px; font-size: 13px; line-height: 1.45; }
    .message.user { justify-self: end; color: #fff; background: #15766b; border-color: #15766b; }
    .menu-options { display: grid; gap: 6px; margin-top: 8px; }
    .menu-btn { padding: 6px 10px; color: #17594f; background: #e6f2ed; border: 1px solid #a8d8ce; border-radius: 5px; font-size: 12px; font-weight: 600; text-align: left; }
    .menu-btn:hover { background: #d0e7de; }
    .loading-indicator { font-size: 11px; color: #718078; font-style: italic; padding: 4px 8px; }
    form { display: flex; gap: 8px; padding: 12px; border-top: 1px solid #d8ded6; background: #fffef9; }
    input { min-width: 0; flex: 1; padding: 9px 12px; border: 1px solid #bfc9bf; border-radius: 5px; font-size: 13px; }
    form button { padding: 9px 14px; color: #18201f; background: #f2c45e; border: 0; border-radius: 5px; font-weight: 700; font-size: 13px; }
  `;

  private async sendMessage(text: string, choice?: string) {
    if (!text && !choice) return;

    const userEntry: ChatMessage = {
      role: "user",
      text: choice ? `Option: ${text}` : text,
    };
    this.messages = [...this.messages, userEntry];
    this.saveSession();
    this.loading = true;

    this.dispatchEvent(
      new CustomEvent("bot-dialog-generator-message", {
        detail: { text, choice, userId: this.userId },
        bubbles: true,
        composed: true,
      })
    );

    try {
      const payload: Record<string, string> = {
        tenant: this.tenant,
        channel: "webchat",
        user_id: this.userId,
      };
      if (choice) {
        payload.choice = choice;
      } else {
        payload.text = text;
      }

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
      // Local fallback simulation if gateway server is unreachable
    }

    // Local deterministic responses
    setTimeout(() => {
      this.loading = false;
      const lower = text.toLowerCase();
      if (lower.includes("balance") || choice === "balance") {
        this.messages = [
          ...this.messages,
          { role: "bot", text: "Your current checking balance is $1,250.50 USD." },
        ];
      } else if (choice === "contact_center" || lower.includes("person") || lower.includes("agent")) {
        this.messages = [
          ...this.messages,
          { role: "bot", text: "Connecting you with an available agent from the contact center now..." },
        ];
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

  render() {
    if (!this.open) {
      return html`<button class="launcher" aria-label="Open chat" @click=${() => { this.open = true; }}>+</button>`;
    }
    return html`
      <section class="panel" aria-label=${this.title}>
        <header>
          ${this.title}
          <button aria-label="Close chat" @click=${() => { this.open = false; }}>×</button>
        </header>
        <div class="messages">
          ${this.messages.map(
            (msg) => html`
              <div class="message ${msg.role === "user" ? "user" : ""}">
                <div>${msg.text}</div>
                ${msg.options
                  ? html`
                      <div class="menu-options">
                        ${msg.options.map(
                          (opt) => html`
                            <button
                              class="menu-btn"
                              @click=${() => this.sendMessage(opt.label, opt.id)}
                            >
                              ${opt.label}
                            </button>
                          `
                        )}
                      </div>
                    `
                  : ""}
              </div>
            `
          )}
          ${this.loading ? html`<div class="loading-indicator">Assistant is thinking...</div>` : ""}
        </div>
        <form @submit=${this.send}>
          <input name="message" aria-label="Message" placeholder="Type a message..." autocomplete="off" />
          <button type="submit">Send</button>
        </form>
      </section>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "bot-dialog-generator-chat": BotDialogGeneratorChat;
  }
}