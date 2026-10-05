import { test, expect } from "@playwright/test";

test.describe("Authoring Journey in Studio Web", () => {
  test("signs in, sees only the member's workspace and selects nodes", async ({ page }) => {
    await page.goto("http://localhost:5173");

    // Development sign-in as Alice, owner of Customer service
    await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
    await page.getByRole("button", { name: /Alice/ }).click();

    // Verify brand and prototype indicator
    await expect(page.locator(".brand")).toContainText("Bot_Dialog_Generator");
    await expect(page.locator(".prototype-badge")).toHaveText("PROTOTYPE");

    // Only Alice's workspace is offered
    const workspaceOptions = page.getByLabel("Workspace").locator("option");
    await expect(workspaceOptions).toHaveText(["Customer service"]);
    await expect(page.locator(".role-pill")).toHaveText("owner");

    // Verify nodes rendered from the Studio API
    const flowNodes = page.locator(".flow-node");
    await expect(flowNodes).toHaveCount(4);

    // Initial inspector shows the entry Trigger
    const inspectorHeading = page.locator(".inspector h2");
    await expect(inspectorHeading).toHaveText("Trigger");

    // Select Service node ("Check balance")
    await page.locator(".flow-node", { hasText: "Check balance" }).click();
    await expect(inspectorHeading).toHaveText("Service");

    // Select Handoff node ("Connect to advisor")
    await page.locator(".flow-node", { hasText: "Connect to advisor" }).click();
    await expect(inspectorHeading).toHaveText("Response");
  });

  test("analysts get a read-only designer", async ({ page }) => {
    await page.goto("http://localhost:5173");
    await page.getByRole("button", { name: /Carol/ }).click();
    await expect(page.locator(".role-pill")).toHaveText("analyst");
    await expect(page.getByRole("button", { name: "Publish bot" })).toBeDisabled();
    await expect(page.getByRole("button", { name: "Save draft" })).toBeDisabled();
  });
});
