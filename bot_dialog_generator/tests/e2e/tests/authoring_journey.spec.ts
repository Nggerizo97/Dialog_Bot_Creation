import { test, expect } from "@playwright/test";

test.describe("Authoring Journey in Studio Web", () => {
  test("loads Studio with prototype banner and verifies node inspector selection", async ({ page }) => {
    await page.goto("http://localhost:5173");

    // Verify brand and prototype indicator
    await expect(page.locator(".brand")).toContainText("Bot_Dialog_Generator");
    await expect(page.locator(".prototype-badge")).toHaveText("PROTOTYPE");

    // Verify Canvas presence
    const canvas = page.locator(".canvas");
    await expect(canvas).toBeVisible();

    // Verify nodes rendered
    const flowNodes = page.locator(".flow-node");
    await expect(flowNodes).toHaveCount(4);

    // Initial inspector shows Menu
    const inspectorHeading = page.locator(".inspector h2");
    await expect(inspectorHeading).toHaveText("Menu");

    // Select Service node ("Check balance")
    const balanceNode = page.locator(".flow-node", { hasText: "Check balance" });
    await balanceNode.click();
    await expect(inspectorHeading).toHaveText("Service");

    // Select Handoff node ("Connect to advisor")
    const handoffNode = page.locator(".flow-node", { hasText: "Connect to advisor" });
    await handoffNode.click();
    await expect(inspectorHeading).toHaveText("Response");
  });
});
