import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { App } from "./App";

describe("Studio App", () => {
  it("renders the prototype indicator in the top header", () => {
    render(<App />);
    const prototypeBadge = screen.getByText("PROTOTYPE");
    expect(prototypeBadge).toBeDefined();
    expect(screen.getByText("Studio")).toBeDefined();
  });

  it("starts with the menu node selected in the inspector", () => {
    render(<App />);
    const inspectorHeading = screen.getByRole("heading", { level: 2 });
    expect(inspectorHeading.textContent).toBe("Menu");
  });

  it("updates inspector when selecting another node", () => {
    render(<App />);

    // Click on the Service node ('Check balance')
    const serviceNode = screen.getByText("Check balance");
    fireEvent.click(serviceNode);

    const inspectorHeading = screen.getByRole("heading", { level: 2 });
    expect(inspectorHeading.textContent).toBe("Service");

    // Click on the Handoff node ('Connect to advisor') within the flow-node buttons
    const handoffNode = screen.getAllByText("Connect to advisor").find((el) => el.closest("button.flow-node"));
    expect(handoffNode).toBeDefined();
    fireEvent.click(handoffNode!);

    expect(inspectorHeading.textContent).toBe("Response");
  });

  it("renders all four initial canvas nodes", () => {
    render(<App />);
    expect(screen.getByText("Welcome")).toBeDefined();
    expect(screen.getByText("What can we help with?")).toBeDefined();
    expect(screen.getByText("Check balance")).toBeDefined();
    expect(screen.getAllByText("Connect to advisor").length).toBeGreaterThanOrEqual(1);
  });
});
