import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import "./bot-dialog-generator-chat";
import { BotDialogGeneratorChat } from "./bot-dialog-generator-chat";

describe("BotDialogGeneratorChat Component", () => {
  let element: BotDialogGeneratorChat;

  beforeEach(() => {
    document.body.innerHTML = "";
    localStorage.clear();
    sessionStorage.clear();
    // No gateway in unit tests: the widget answers with its local fallback.
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")));
    element = document.createElement("bot-dialog-generator-chat") as BotDialogGeneratorChat;
    document.body.appendChild(element);
  });

  it("renders launcher button initially", async () => {
    await element.updateComplete;
    const launcher = element.shadowRoot?.querySelector(".launcher");
    expect(launcher).toBeDefined();
    expect(launcher?.textContent?.trim()).toBe("+");
  });

  it("opens the panel when clicking launcher", async () => {
    await element.updateComplete;
    const launcher = element.shadowRoot?.querySelector(".launcher") as HTMLButtonElement;
    launcher.click();
    await element.updateComplete;

    const panel = element.shadowRoot?.querySelector(".panel");
    expect(panel).toBeDefined();
    const header = element.shadowRoot?.querySelector("header");
    expect(header?.textContent).toContain("Bot_Dialog_Generator assistant");
  });

  it("dispatches bot-dialog-generator-message event on submitting input", async () => {
    await element.updateComplete;
    // Open chat
    (element.shadowRoot?.querySelector(".launcher") as HTMLButtonElement).click();
    await element.updateComplete;

    let receivedDetail: any = null;
    element.addEventListener("bot-dialog-generator-message", (e: any) => {
      receivedDetail = e.detail;
    });

    const form = element.shadowRoot?.querySelector("form") as HTMLFormElement;
    const input = form.querySelector("input") as HTMLInputElement;
    input.value = "Hello Bot_Dialog_Generator";

    form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await element.updateComplete;

    expect(receivedDetail).toBeDefined();
    expect(receivedDetail.text).toBe("Hello Bot_Dialog_Generator");
    expect(receivedDetail.userId).toBeDefined();
  });

  it("persists conversation messages across instances using same user ID", async () => {
    await element.updateComplete;
    (element.shadowRoot?.querySelector(".launcher") as HTMLButtonElement).click();
    await element.updateComplete;

    // Simulate sending message
    const form = element.shadowRoot?.querySelector("form") as HTMLFormElement;
    const input = form.querySelector("input") as HTMLInputElement;
    input.value = "Persistent query";
    form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    await element.updateComplete;

    // Create second instance representing a page reload
    const secondElement = document.createElement("bot-dialog-generator-chat") as BotDialogGeneratorChat;
    document.body.appendChild(secondElement);
    await secondElement.updateComplete;

    // Verify messages restored
    const stored = sessionStorage.getItem(`bot_dialog_generator_messages_${(element as any).userId}`);
    expect(stored).toContain("Persistent query");
    // Nothing outlives the tab.
    expect(Object.keys(localStorage).filter((k) => k.startsWith("bot_dialog_generator"))).toEqual([]);
  });

  it("offers the AI assistant or a contact center agent before transferring", async () => {
    {
      const chat = element as any;
      const lastBot = () => chat.messages.filter((m: any) => m.role === "bot").at(-1);

      await chat.sendMessage("Connect to advisor", "handoff");
      await vi.waitFor(() => expect(chat.loading).toBe(false));
      expect(lastBot().options.map((o: any) => o.id)).toEqual(["ai_assistant", "contact_center"]);

      await chat.sendMessage("AI assistant", "ai_assistant");
      await vi.waitFor(() => expect(chat.loading).toBe(false));
      expect(lastBot().options[0]).toEqual({ id: "contact_center", label: "Talk to a person" });

      await chat.sendMessage("Talk to a person", "contact_center");
      await vi.waitFor(() => expect(chat.loading).toBe(false));
      expect(lastBot().text).toBe("Connecting you with an available agent from the contact center now...");
    }
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

  it("says it is an automated assistant and links the privacy notice when given", async () => {
    element.setAttribute("privacy-url", "https://example.com/privacy");
    (element as any).open = true;
    await element.updateComplete;
    const disclosure = element.shadowRoot?.querySelector(".disclosure");
    expect(disclosure?.textContent).toContain("automated assistant, not a person");
    expect(disclosure?.querySelector("a")?.getAttribute("href")).toBe("https://example.com/privacy");
  });

  it("announces new messages to screen readers", async () => {
    (element as any).open = true;
    await element.updateComplete;
    const log = element.shadowRoot?.querySelector(".messages");
    expect(log?.getAttribute("role")).toBe("log");
    expect(log?.getAttribute("aria-live")).toBe("polite");
  });

  it("closes with Escape and returns focus to the launcher", async () => {
    (element as any).open = true;
    await element.updateComplete;
    const panel = element.shadowRoot?.querySelector(".panel") as HTMLElement;
    panel.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await vi.waitFor(() => expect(element.shadowRoot?.querySelector(".panel")).toBeNull());
    expect(element.shadowRoot?.activeElement).toBe(element.shadowRoot?.querySelector(".launcher"));
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
});
