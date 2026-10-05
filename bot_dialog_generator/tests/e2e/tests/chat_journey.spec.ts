import { test, expect } from "@playwright/test";

test.describe("Chat Journey in Webchat Widget", () => {
  test("opens widget launcher, enters message, and dispatches message event", async ({ page }) => {
    // Open the webchat test harness host page
    await page.goto("http://localhost:5174");

    // Initially launcher button is visible
    const launcher = page.locator("bot-dialog-generator-chat").locator(".launcher");
    await expect(launcher).toBeVisible();

    // Click launcher to open chat panel
    await launcher.click();

    // Panel is opened
    const panel = page.locator("bot-dialog-generator-chat").locator(".panel");
    await expect(panel).toBeVisible();

    // Message list displays initial welcome message
    const messages = page.locator("bot-dialog-generator-chat").locator(".message");
    await expect(messages.first()).toContainText("Hi, how can we help today?");

    // Type a message in the input and submit
    const input = page.locator("bot-dialog-generator-chat").locator('input[name="message"]');
    await input.fill("I want to check my account balance");
    await page.locator("bot-dialog-generator-chat").locator("form button").click();

    // The user message appears in the conversation
    await expect(page.locator("bot-dialog-generator-chat").locator(".message.user")).toContainText("I want to check my account balance");
  });
});
