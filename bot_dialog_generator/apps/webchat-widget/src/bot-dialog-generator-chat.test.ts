import { describe, it, expect, beforeEach, vi } from "vitest";
import "./bot-dialog-generator-chat";
import { BotDialogGeneratorChat } from "./bot-dialog-generator-chat";

describe("BotDialogGeneratorChat Component", () => {
  let element: BotDialogGeneratorChat;

  beforeEach(() => {
    document.body.innerHTML = "";
    localStorage.clear();
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
    const stored = localStorage.getItem(`bot_dialog_generator_messages_${(element as any).userId}`);
    expect(stored).toContain("Persistent query");
  });

  it("offers the AI assistant or a contact center agent before transferring", async () => {
    // Gateway unreachable: the widget answers with its local fallback script.
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("offline")));
    try {
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
    } finally {
      vi.unstubAllGlobals();
    }
  });
});
