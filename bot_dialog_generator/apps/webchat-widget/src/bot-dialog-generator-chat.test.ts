import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import "./bot-dialog-generator-chat";
import { BotDialogGeneratorChat, STORAGE_KEYS } from "./bot-dialog-generator-chat";
import cookiesDoc from "../../../docs/legal/COOKIES_AND_STORAGE.md?raw";

type Attrs = Record<string, string | true>;

/** Requests the widget made, with their parsed JSON bodies. */
interface Call {
  url: string;
  body: any;
}

let calls: Call[] = [];

/** Stubs fetch. `respond` returns the JSON for a URL, or undefined to fail like an unreachable server. */
function stubFetch(respond: (url: string) => unknown = () => undefined) {
  calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      calls.push({ url, body: init?.body ? JSON.parse(String(init.body)) : undefined });
      const data = respond(url);
      if (data === undefined) throw new Error("offline");
      return new Response(JSON.stringify(data), { status: 200, headers: { "Content-Type": "application/json" } });
    }),
  );
}

const gatewayUp = (url: string) => {
  if (url.endsWith("/messages")) return { messages: [{ kind: "text", text: "Hello from the gateway" }] };
  if (url.endsWith("/consent")) return {};
  if (url.endsWith("/data-deletion")) return { reference: "req-123", status: "completed", server_copy: true };
  if (url.endsWith("/events")) return {};
  return undefined;
};

async function mount(attrs: Attrs = {}): Promise<BotDialogGeneratorChat> {
  const element = document.createElement("bot-dialog-generator-chat") as BotDialogGeneratorChat;
  for (const [name, value] of Object.entries(attrs)) element.setAttribute(name, value === true ? "" : value);
  document.body.appendChild(element);
  await element.updateComplete;
  return element;
}

const $ = <T extends Element = HTMLElement>(el: BotDialogGeneratorChat, selector: string) => el.shadowRoot!.querySelector<T>(selector);
const $$ = <T extends Element = HTMLElement>(el: BotDialogGeneratorChat, selector: string) => [...el.shadowRoot!.querySelectorAll<T>(selector)];
const button = (el: BotDialogGeneratorChat, name: string) =>
  $$<HTMLButtonElement>(el, "button").find((b) => (b.getAttribute("aria-label") ?? b.textContent ?? "").trim() === name);
const active = (el: BotDialogGeneratorChat) => el.shadowRoot!.activeElement;

async function open(el: BotDialogGeneratorChat) {
  $<HTMLButtonElement>(el, ".launcher")!.click();
  await el.updateComplete;
}

async function submit(el: BotDialogGeneratorChat, selector: string) {
  $(el, selector)!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  await el.updateComplete;
}

async function say(el: BotDialogGeneratorChat, text: string) {
  $<HTMLInputElement>(el, 'input[name="message"]')!.value = text;
  await submit(el, "form.composer");
  await vi.waitFor(() => expect((el as any).loading).toBe(false));
  await el.updateComplete;
}

const press = (el: BotDialogGeneratorChat, key: string, init: KeyboardEventInit = {}) =>
  $(el, ".panel")!.dispatchEvent(new KeyboardEvent("keydown", { key, bubbles: true, cancelable: true, ...init }));

beforeEach(() => {
  document.body.innerHTML = "";
  localStorage.clear();
  sessionStorage.clear();
  window.history.replaceState(null, "", "/");
  // No gateway unless a test says so.
  stubFetch();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Chat basics", () => {
  it("renders a labelled launcher whose icon is hidden from screen readers", async () => {
    const el = await mount();
    const launcher = $(el, ".launcher")!;
    expect(launcher.getAttribute("aria-label")).toBe("Open chat");
    expect(launcher.querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
  });

  it("opens as a named dialog and says it is an automated assistant", async () => {
    const el = await mount();
    await open(el);
    const panel = $(el, ".panel")!;
    expect(panel.getAttribute("role")).toBe("dialog");
    expect(panel.getAttribute("aria-labelledby")).toBe("chat-title");
    expect($(el, "#chat-title")!.textContent).toContain("Bot_Dialog_Generator assistant");
    expect($(el, ".disclosure")!.textContent).toContain("automated assistant, not a person");
  });

  it("gives the conversation an ID that cannot be guessed and means nothing", async () => {
    const el = await mount();
    expect((el as any).userId).toMatch(/^usr-[0-9a-f]{32}$/);
    const other = await mount();
    // The same tab keeps one conversation.
    expect((other as any).userId).toBe((el as any).userId);
  });

  it("keeps the conversation for the tab only", async () => {
    stubFetch(gatewayUp);
    const el = await mount();
    await open(el);
    await say(el, "Persistent query");

    const reloaded = await mount();
    expect((reloaded as any).messages.map((m: any) => m.text)).toContain("Persistent query");
    expect(sessionStorage.getItem(`${STORAGE_KEYS.messagesPrefix}${(el as any).userId}`)).toContain("Persistent query");
    // Nothing outlives the tab.
    expect(Object.keys(localStorage).filter((k) => k.startsWith("bot_dialog_generator"))).toEqual([]);
  });

  it("removes conversations that older versions kept in localStorage", () => {
    localStorage.setItem("bot_dialog_generator_chat_user_id", "usr-old");
    localStorage.setItem("bot_dialog_generator_messages_usr-old", "[]");
    localStorage.setItem("unrelated", "kept");
    document.createElement("bot-dialog-generator-chat");
    expect(localStorage.getItem("bot_dialog_generator_chat_user_id")).toBeNull();
    expect(localStorage.getItem("bot_dialog_generator_messages_usr-old")).toBeNull();
    expect(localStorage.getItem("unrelated")).toBe("kept");
  });

  it("links the legal documents from the footer and the menu, and only the ones configured", async () => {
    const el = await mount({ "privacy-url": "https://example.com/privacy", "terms-url": "https://example.com/terms" });
    await open(el);
    const footerLinks = $$<HTMLAnchorElement>(el, "footer a");
    expect(footerLinks.map((a) => a.getAttribute("href"))).toEqual(["https://example.com/privacy", "https://example.com/terms"]);
    for (const a of footerLinks) {
      expect(a.getAttribute("rel")).toBe("noopener noreferrer");
      expect(a.textContent).toContain("(opens in a new tab)");
    }
    button(el, "Chat options")!.click();
    await el.updateComplete;
    expect($$(el, ".menu a").map((a) => a.textContent!.trim().replace(/\s*\(opens in a new tab\)/, ""))).toEqual(["Privacy Policy", "Terms of Use"]);

    const bare = await mount();
    await open(bare);
    expect($(bare, "footer")).toBeNull();
  });
});

describe("Honest answers", () => {
  it("never makes up a reply when the assistant cannot be reached", async () => {
    const el = await mount();
    await open(el);
    await say(el, "What is my balance?");
    const messages = (el as any).messages;
    expect(messages.at(-1).role).toBe("error");
    expect(messages.some((m: any) => /balance of|\$/.test(m.text) && m.role === "bot")).toBe(false);
    expect($(el, ".message.error")!.getAttribute("role")).toBe("alert");
  });

  it("labels demo replies as sample data and says no agent will join", async () => {
    const el = await mount({ demo: true });
    const chat = el as any;
    const lastBot = () => chat.messages.filter((m: any) => m.role === "bot").at(-1);

    await chat.sendMessage("Check balance", "balance");
    await vi.waitFor(() => expect(chat.loading).toBe(false));
    expect(lastBot().text).toContain("Sample data");
    expect(lastBot().text).toContain("not connected to any real account");

    await chat.sendMessage("Connect to advisor", "handoff");
    await vi.waitFor(() => expect(chat.loading).toBe(false));
    expect(lastBot().options.map((o: any) => o.id)).toEqual(["ai_assistant", "contact_center"]);

    await chat.sendMessage("AI assistant", "ai_assistant");
    await vi.waitFor(() => expect(chat.loading).toBe(false));
    expect(lastBot().options[0]).toEqual({ id: "contact_center", label: "Talk to a person" });

    await chat.sendMessage("Talk to a person", "contact_center");
    await vi.waitFor(() => expect(chat.loading).toBe(false));
    expect(lastBot().text).toContain("no agent will join");
  });
});

describe("Data minimization", () => {
  it("tells the page that a message was sent, not what it said", async () => {
    stubFetch(gatewayUp);
    const el = await mount();
    await open(el);
    let detail: any = null;
    el.addEventListener("bot-dialog-generator-message", (e: any) => (detail = e.detail));
    await say(el, "My ID number is 1234567");
    expect(detail.kind).toBe("text");
    expect(JSON.stringify(detail)).not.toContain("1234567");
    expect(detail.userId).toBeUndefined();
  });

  it("shares the text with the page only when the site opts in", async () => {
    stubFetch(gatewayUp);
    const el = await mount({ "expose-message-text": true });
    await open(el);
    let detail: any = null;
    el.addEventListener("bot-dialog-generator-message", (e: any) => (detail = e.detail));
    await say(el, "Hello Bot_Dialog_Generator");
    expect(detail.text).toBe("Hello Bot_Dialog_Generator");
  });

  it("sends the gateway only the conversation ID, the message and the consent flags", async () => {
    stubFetch(gatewayUp);
    const el = await mount();
    await open(el);
    await say(el, "Hello");
    const sent = calls.find((c) => c.url.endsWith("/messages"))!.body;
    expect(Object.keys(sent).sort()).toEqual(["channel", "consent", "tenant", "text", "user_id"]);
    expect(sent.consent).toEqual({ analytics: false, campaigns: false });
  });
});

describe("Pre-chat consent", () => {
  const gated: Attrs = { "require-authorization": true, "min-age": "16", "privacy-url": "https://example.com/privacy", "terms-url": "https://example.com/terms" };

  it("shows nothing to accept when the site asks for nothing", async () => {
    const el = await mount();
    await open(el);
    expect($(el, ".gate")).toBeNull();
    expect($(el, 'input[name="message"]')).not.toBeNull();
  });

  it("starts with every box unchecked and links the Privacy Policy and Terms", async () => {
    const el = await mount({ ...gated, "analytics-url": "https://stats.example.com", "campaign-attribution": true });
    await open(el);
    const boxes = $$<HTMLInputElement>(el, '.gate input[type="checkbox"]');
    expect(boxes.map((b) => b.name)).toEqual(["processing", "age", "analytics", "campaigns"]);
    expect(boxes.every((b) => b.checked === false)).toBe(true);
    expect(boxes.filter((b) => b.required).map((b) => b.name)).toEqual(["processing", "age"]);
    expect($$<HTMLAnchorElement>(el, ".gate a").map((a) => a.getAttribute("href"))).toEqual(["https://example.com/privacy", "https://example.com/terms"]);
    expect($(el, ".gate")!.textContent).toContain("I am 16 or older.");
    // Each box is labelled by the sentence next to it.
    expect(boxes.every((b) => b.closest("label") !== null)).toBe(true);
  });

  it("does not start the chat or send anything until the required boxes are ticked", async () => {
    stubFetch(gatewayUp);
    const el = await mount(gated);
    await open(el);
    expect($(el, 'input[name="message"]')).toBeNull();

    await submit(el, ".gate");
    expect($(el, ".form-error")!.getAttribute("role")).toBe("alert");
    expect((active(el) as HTMLInputElement).name).toBe("processing");
    expect(calls).toEqual([]);
    // Even a direct call cannot get a message past the gate.
    await (el as any).sendMessage("hello");
    expect(calls).toEqual([]);
  });

  it("records what the person agreed to, with unticked optional boxes as refused", async () => {
    stubFetch(gatewayUp);
    const el = await mount({ ...gated, "analytics-url": "https://stats.example.com", "policy-version": "2026-11" });
    await open(el);
    $<HTMLInputElement>(el, 'input[name="processing"]')!.checked = true;
    $<HTMLInputElement>(el, 'input[name="age"]')!.checked = true;
    await submit(el, ".gate");
    // Focus moves to the message box once the chat starts.
    await vi.waitFor(() => expect(active(el)).toBe($(el, 'input[name="message"]')));

    const proof = calls.find((c) => c.url.endsWith("/channels/webchat/consent"))!.body;
    expect(proof).toMatchObject({
      user_id: (el as any).userId,
      purposes: { processing: true, analytics: false },
      policy_version: "2026-11",
      age_confirmed: true,
      guardian_consent: false,
    });
    // The choice is remembered for this tab, so a reload does not ask again.
    expect(JSON.parse(sessionStorage.getItem(STORAGE_KEYS.consent)!)).toMatchObject({ accepted: true, analytics: false });
    const reloaded = await mount(gated);
    await open(reloaded);
    expect($(reloaded, ".gate")).toBeNull();
  });

  it("does not start when the proof of consent could not be saved", async () => {
    const el = await mount(gated); // gateway unreachable
    await open(el);
    $<HTMLInputElement>(el, 'input[name="processing"]')!.checked = true;
    $<HTMLInputElement>(el, 'input[name="age"]')!.checked = true;
    await submit(el, ".gate");
    await vi.waitFor(() => expect($(el, ".form-error")).not.toBeNull());
    expect($(el, ".form-error")!.textContent).toContain("could not save your choice");
    expect($(el, 'input[name="message"]')).toBeNull();
    expect(sessionStorage.getItem(STORAGE_KEYS.consent)).toBeNull();
  });

  it("gives 'Start chat' and 'Not now' the same weight, and 'Not now' closes in one click", async () => {
    const el = await mount(gated);
    await open(el);
    const [start, notNow] = $$<HTMLButtonElement>(el, ".gate .choices button");
    expect([start.textContent!.trim(), notNow.textContent!.trim()]).toEqual(["Start chat", "Not now"]);
    expect(start.className).toBe(notNow.className);
    notNow.click();
    await vi.waitFor(() => expect($(el, ".panel")).toBeNull());
  });
});

describe("Minors and parental consent", () => {
  it("asks a parent or guardian, not the child, when the bot is for minors", async () => {
    stubFetch(gatewayUp);
    const el = await mount({ audience: "minors", "min-age": "14" });
    await open(el);
    const boxes = $$<HTMLInputElement>(el, '.gate input[type="checkbox"]');
    expect(boxes.map((b) => b.name)).toEqual(["guardian"]);
    expect(boxes[0].checked).toBe(false);
    expect($(el, ".gate")!.textContent).toContain("parent or legal guardian");

    await submit(el, ".gate");
    expect($(el, 'input[name="message"]')).toBeNull();

    boxes[0].checked = true;
    await submit(el, ".gate");
    await vi.waitFor(() => expect($(el, 'input[name="message"]')).not.toBeNull());
    expect(calls.find((c) => c.url.endsWith("/consent"))!.body).toMatchObject({ guardian_consent: true, age_confirmed: false });
  });

  it("waits for the page's own parental-consent process when the site verifies guardians itself", async () => {
    stubFetch(gatewayUp);
    const el = await mount({ audience: "minors", "parental-consent": "host" });
    await open(el);
    expect($$(el, '.gate input[type="checkbox"]')).toEqual([]);
    await submit(el, ".gate");
    expect($(el, ".form-error")!.textContent).toContain("parent or guardian needs to give permission");
    expect($(el, 'input[name="message"]')).toBeNull();

    el.grantParentalConsent("verification-789");
    await el.updateComplete;
    await submit(el, ".gate");
    await vi.waitFor(() => expect($(el, 'input[name="message"]')).not.toBeNull());
    expect(calls.find((c) => c.url.endsWith("/consent"))!.body).toMatchObject({ guardian_consent: true, guardian_reference: "verification-789" });
  });
});

describe("Analytics and campaign opt-ins", () => {
  const optional: Attrs = { "analytics-url": "https://stats.example.com/", "campaign-attribution": true };
  const analyticsCalls = () => calls.filter((c) => c.url.startsWith("https://stats.example.com"));

  it("does not offer an opt-in for something the site does not use", async () => {
    const el = await mount();
    await open(el);
    expect($(el, ".prompt")).toBeNull();
  });

  it("asks without blocking the chat, with two equal buttons, and sends nothing before an answer", async () => {
    stubFetch(gatewayUp);
    window.history.replaceState(null, "", "/?utm_campaign=spring&utm_source=newsletter");
    const el = await mount(optional);
    await open(el);
    const [allow, decline] = $$<HTMLButtonElement>(el, ".prompt .choices button");
    expect([allow.textContent!.trim(), decline.textContent!.trim()]).toEqual(["Allow", "No thanks"]);
    expect(allow.className).toBe(decline.className);

    // The chat already works, and nothing optional leaves the browser.
    await say(el, "Hello");
    expect(analyticsCalls()).toEqual([]);
    const sent = calls.find((c) => c.url.endsWith("/messages"))!.body;
    expect(sent.consent).toEqual({ analytics: false, campaigns: false });
    expect(sent.context).toBeUndefined();
  });

  it("keeps everything off after 'No thanks', in one click", async () => {
    stubFetch(gatewayUp);
    window.history.replaceState(null, "", "/?utm_campaign=spring");
    const el = await mount(optional);
    await open(el);
    button(el, "No thanks")!.click();
    await el.updateComplete;
    expect($(el, ".prompt")).toBeNull();
    await say(el, "Hello");
    expect(analyticsCalls()).toEqual([]);
    expect(calls.find((c) => c.url.endsWith("/messages"))!.body.context).toBeUndefined();
    expect(calls.find((c) => c.url.endsWith("/consent"))!.body.purposes).toEqual({ analytics: false, campaigns: false });
  });

  it("counts events and campaign tags only after 'Allow', and never the words", async () => {
    stubFetch(gatewayUp);
    window.history.replaceState(null, "", "/?utm_campaign=spring&utm_source=news letter&gclid=abc");
    const el = await mount(optional);
    await open(el);
    let announced: any = null;
    el.addEventListener("bot-dialog-generator-consent", (e: any) => (announced = e.detail));
    button(el, "Allow")!.click();
    await el.updateComplete;
    expect(announced).toEqual({ analytics: true, campaigns: true });

    await say(el, "My card number is 4111 1111 1111 1111");
    const events = analyticsCalls();
    expect(events.map((c) => c.url)).toEqual(["https://stats.example.com/events"]);
    expect(events[0].body).toEqual({
      source: "webchat",
      event: "message_sent",
      consent: { analytics: true, campaigns: true },
      // utm_source had a space, so it is not a tag; gclid is not on the list.
      props: { channel: "webchat", utm_campaign: "spring" },
    });
    expect(JSON.stringify(events)).not.toContain("4111");
    expect(JSON.stringify(events)).not.toContain((el as any).userId);
    expect(calls.find((c) => c.url.endsWith("/messages"))!.body.context).toEqual({ utm_campaign: "spring" });
  });

  it("lets the person change their mind under 'Privacy choices'", async () => {
    stubFetch(gatewayUp);
    const el = await mount(optional);
    await open(el);
    button(el, "Allow")!.click();
    await el.updateComplete;

    button(el, "Chat options")!.click();
    await el.updateComplete;
    button(el, "Privacy choices")!.click();
    await el.updateComplete;
    const analytics = $<HTMLInputElement>(el, 'input[name="analytics"]')!;
    expect(analytics.checked).toBe(true);
    analytics.checked = false;
    await submit(el, "form.card");
    await say(el, "Hello");
    expect(analyticsCalls()).toEqual([]);
  });
});

describe("Deleting and leaving", () => {
  async function openDeleteConfirmation(el: BotDialogGeneratorChat) {
    button(el, "Chat options")!.click();
    await el.updateComplete;
    button(el, "Delete my data")!.click();
    await el.updateComplete;
  }

  it("deletes the conversation here and on the servers, and shows the reference", async () => {
    stubFetch(gatewayUp);
    const el = await mount();
    await open(el);
    await say(el, "Please remember this");
    const oldId = (el as any).userId;
    let deleted: any = null;
    el.addEventListener("bot-dialog-generator-data-deleted", (e: any) => (deleted = e.detail));

    await openDeleteConfirmation(el);
    const confirmation = $(el, '[role="alertdialog"]')!;
    expect(confirmation.textContent).toContain("cannot be undone");
    const [confirm, keep] = $$<HTMLButtonElement>(el, ".card .choices button");
    expect([confirm.textContent!.trim(), keep.textContent!.trim()]).toEqual(["Delete my data", "Keep my data"]);
    expect(confirm.className).toBe(keep.className);

    confirm.click();
    await vi.waitFor(() => expect($(el, ".message.notice")).not.toBeNull());
    expect(calls.find((c) => c.url.endsWith("/channels/webchat/data-deletion"))!.body).toEqual({ user_id: oldId });
    expect($(el, ".message.notice")!.textContent).toContain("Reference: req-123");
    expect(deleted).toEqual({ reference: "req-123" });
    expect(sessionStorage.getItem(`${STORAGE_KEYS.messagesPrefix}${oldId}`)).toBeNull();
    expect(sessionStorage.getItem(STORAGE_KEYS.consent)).toBeNull();
    expect((el as any).userId).not.toBe(oldId);
    expect(JSON.stringify((el as any).messages)).not.toContain("Please remember this");
  });

  it("does not claim a deletion it could not confirm", async () => {
    stubFetch((url) => (url.endsWith("/messages") ? gatewayUp(url) : undefined));
    const el = await mount();
    await open(el);
    await say(el, "Please remember this");
    const oldId = (el as any).userId;

    await openDeleteConfirmation(el);
    button(el, "Delete my data")!.click();
    await vi.waitFor(() => expect($(el, ".form-error")).not.toBeNull());
    expect($(el, ".form-error")!.textContent).toContain("nothing was deleted yet");
    expect((el as any).userId).toBe(oldId);
    expect(sessionStorage.getItem(`${STORAGE_KEYS.messagesPrefix}${oldId}`)).toContain("Please remember this");

    button(el, "Keep my data")!.click();
    await el.updateComplete;
    expect($(el, 'input[name="message"]')).not.toBeNull();
  });

  it("clears the conversation from the browser when the chat is ended", async () => {
    stubFetch(gatewayUp);
    const el = await mount();
    await open(el);
    await say(el, "Temporary");
    const oldId = (el as any).userId;
    button(el, "Chat options")!.click();
    await el.updateComplete;
    button(el, "End chat and clear it")!.click();
    await vi.waitFor(() => expect($(el, ".panel")).toBeNull());
    expect(sessionStorage.getItem(`${STORAGE_KEYS.messagesPrefix}${oldId}`)).toBeNull();
    expect(sessionStorage.getItem(STORAGE_KEYS.user)).toBeNull();
  });

  it("styles every menu entry alike, so leaving and deleting are as easy to find as the rest", async () => {
    const el = await mount({ "privacy-url": "https://example.com/privacy", "analytics-url": "https://stats.example.com" });
    await open(el);
    button(el, "Chat options")!.click();
    await el.updateComplete;
    const items = $$(el, '.menu [role="menuitem"]');
    expect(items.map((i) => i.textContent!.replace(/\s*\(opens in a new tab\)/, "").trim())).toEqual([
      "Privacy Policy",
      "Privacy choices",
      "Delete my data",
      "End chat and clear it",
    ]);
    expect(new Set(items.map((i) => i.className)).size).toBe(1);
  });
});

describe("Keyboard and screen readers", () => {
  it("announces new messages politely and names who is speaking", async () => {
    stubFetch(gatewayUp);
    const el = await mount();
    await open(el);
    const log = $(el, ".messages")!;
    expect(log.getAttribute("role")).toBe("log");
    expect(log.getAttribute("aria-live")).toBe("polite");
    await say(el, "Hello");
    const spoken = $$(el, ".message").map((m) => m.textContent!.replace(/\s+/g, " ").trim());
    expect(spoken).toEqual(["Assistant: Hi, how can we help today?", "You: Hello", "Assistant: Hello from the gateway"]);
  });

  it("labels every button that shows only an icon", async () => {
    const el = await mount();
    await open(el);
    for (const b of $$<HTMLButtonElement>(el, "button")) {
      const name = (b.getAttribute("aria-label") ?? b.textContent ?? "").trim();
      expect(name.length, `button ${b.outerHTML} has no accessible name`).toBeGreaterThan(1);
    }
    expect(button(el, "Close chat")).toBeDefined();
    expect($(el, 'input[name="message"]')!.getAttribute("aria-label")).toBe("Message");
  });

  it("moves focus into the chat when it opens and back to the launcher when it closes", async () => {
    const el = await mount();
    await open(el);
    expect(active(el)).toBe($(el, 'input[name="message"]'));
    press(el, "Escape");
    await vi.waitFor(() => expect($(el, ".panel")).toBeNull());
    expect(active(el)).toBe($(el, ".launcher"));
  });

  it("keeps Tab and Shift+Tab inside the open chat", async () => {
    const el = await mount({ "privacy-url": "https://example.com/privacy" });
    await open(el);
    const first = button(el, "Chat options")!;
    const last = $(el, "footer a")!;

    last.focus();
    press(el, "Tab");
    expect(active(el)).toBe(first);

    press(el, "Tab", { shiftKey: true });
    expect(active(el)).toBe(last);
  });

  it("opens the menu from the keyboard, moves with the arrows, and Escape closes only the menu", async () => {
    const el = await mount();
    await open(el);
    const toggle = button(el, "Chat options")!;
    expect(toggle.getAttribute("aria-haspopup")).toBe("menu");
    expect(toggle.getAttribute("aria-expanded")).toBe("false");

    toggle.click();
    await el.updateComplete;
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    const items = $$(el, '.menu [role="menuitem"]');
    expect(active(el)).toBe(items[0]);
    press(el, "ArrowDown");
    expect(active(el)).toBe(items[1]);
    press(el, "ArrowUp");
    press(el, "ArrowUp");
    expect(active(el)).toBe(items.at(-1));

    press(el, "Escape");
    await vi.waitFor(() => expect($(el, ".menu")).toBeNull());
    expect($(el, ".panel")).not.toBeNull();
    expect(active(el)).toBe(toggle);
  });
});

describe("Browser storage is documented", () => {
  it("lists every key in COOKIES_AND_STORAGE.md", () => {
    expect(cookiesDoc).toContain(`\`${STORAGE_KEYS.user}\``);
    expect(cookiesDoc).toContain(`\`${STORAGE_KEYS.messagesPrefix}<id>\``);
    expect(cookiesDoc).toContain(`\`${STORAGE_KEYS.consent}\``);
  });

  it("writes only those keys, only to sessionStorage, and no cookies", async () => {
    stubFetch(gatewayUp);
    const el = await mount({ "require-authorization": true, "analytics-url": "https://stats.example.com" });
    await open(el);
    $<HTMLInputElement>(el, 'input[name="processing"]')!.checked = true;
    await submit(el, ".gate");
    await vi.waitFor(() => expect($(el, 'input[name="message"]')).not.toBeNull());
    await say(el, "Hello");
    const keys = Object.keys(sessionStorage).map((k) => (k.startsWith(STORAGE_KEYS.messagesPrefix) ? `${STORAGE_KEYS.messagesPrefix}<id>` : k));
    expect(keys.sort()).toEqual([STORAGE_KEYS.consent, STORAGE_KEYS.user, `${STORAGE_KEYS.messagesPrefix}<id>`].sort());
    expect(Object.keys(localStorage)).toEqual([]);
    expect(document.cookie).toBe("");
  });
});
