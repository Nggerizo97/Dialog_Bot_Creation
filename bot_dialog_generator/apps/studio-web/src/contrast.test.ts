import { describe, it, expect } from "vitest";
import css from "./styles.css?raw";
import { contrastRatio, parseRules, solidColor } from "./contrast";

// WCAG 2.1 AA: 4.5:1 for text (1.4.3), 3:1 for what identifies a control or its focus (1.4.11).
const rules = parseRules(css);

const named = (selector: string) => {
  const rule = rules.find((r) => r.selector === selector);
  if (!rule) throw new Error(`styles.css has no rule "${selector}"`);
  return rule.declarations;
};

describe("Studio colors (WCAG 2.1 AA)", () => {
  it("parses the stylesheet", () => {
    expect(rules.length).toBeGreaterThan(80);
  });

  it("every rule that sets both a text color and a background is readable", () => {
    const failures: string[] = [];
    for (const { selector, declarations } of rules) {
      const fg = solidColor(declarations.color);
      const bg = solidColor(declarations.background) ?? solidColor(declarations["background-color"]);
      if (!fg || !bg) continue;
      const ratio = contrastRatio(fg, bg);
      if (ratio < 4.5) failures.push(`${selector}: ${fg} on ${bg} = ${ratio.toFixed(2)}`);
    }
    expect(failures).toEqual([]);
  });

  // Text whose background comes from the element it sits on.
  const textOnSurface: Array<[selector: string, background: string, where: string]> = [
    [".sidebar nav button", "#f6f3ea", "sidebar"],
    [".sidebar-footer", "#f6f3ea", "sidebar"],
    [".eyebrow", "#fafaf5", "page header"],
    [".node-type", "#fffef9", "flow node"],
    [".trigger .node-type", "#fffef9", "flow node"],
    [".service .node-type", "#fffef9", "flow node"],
    [".response .node-type", "#fffef9", "flow node"],
    [".jump .node-type", "#fffef9", "flow node"],
    [".inspector label", "#fffef9", "inspector"],
    [".inspector-note", "#fffef9", "inspector"],
    [".new-flow", "#f6f3ea", "sidebar"],
    [".admin-note", "#fafaf5", "page"],
    [".empty-state", "#fafaf5", "page"],
    [".field-help", "#fffef9", "forms and dialogs"],
    [".login-error", "#fffef9", "sign-in card"],
    [".form-error", "#fffef9", "forms and dialogs"],
    [".status-message", "#18201f", "top bar"],
    [".brand span", "#18201f", "top bar"],
    [".version", "#18201f", "top bar"],
    [".user-chip", "#18201f", "top bar"],
    [".role-pill", "#18201f", "top bar"],
    [".site-footer address", "#18201f", "footer"],
    [".active-pointer", "#ffffff", "versions table"],
    [".legal-nav ul a", "#fafaf5", "legal pages"],
  ];

  it.each(textOnSurface)("%s is readable on the %s background (%s)", (selector, background) => {
    const fg = solidColor(named(selector).color);
    expect(fg, `${selector} sets no color`).not.toBeNull();
    expect(contrastRatio(fg!, background)).toBeGreaterThanOrEqual(4.5);
  });

  // Borders that show where a control is, against what surrounds it.
  const controlBorders: Array<[selector: string, surrounding: string]> = [
    [".inspector input, .inspector textarea, .inspector select", "#f7f8f3"],
    [".inline-form input, .inline-form select, .versions-table select", "#f7f8f3"],
    [".test-input-form input", "#ffffff"],
    [".flow-node", "#edf0eb"],
    [".add-node", "#edf0eb"],
    [".new-flow", "#f6f3ea"],
    [".action-btn", "#ffffff"],
    [".delete-node-btn", "#fffef9"],
    [".test-menu-btn", "#f7f9f7"],
    [".demo-users button", "#fffef9"],
    [".picker", "#18201f"],
    [".choice-button", "#fffef9"],
    [".privacy-banner", "#edf0eb"],
  ];

  it.each(controlBorders)("%s has a visible border against %s", (selector, surrounding) => {
    const border = solidColor(named(selector).border);
    expect(border, `${selector} sets no solid border color`).not.toBeNull();
    expect(contrastRatio(border!, surrounding)).toBeGreaterThanOrEqual(3);
  });

  it("focus rings stand out on every background they appear on", () => {
    const light = solidColor(named(":where(button, a, select, input, textarea, summary, [tabindex]):focus-visible").outline)!;
    for (const background of ["#edf0eb", "#f6f3ea", "#fafaf5", "#fffef9", "#ffffff"]) {
      expect(contrastRatio(light, background)).toBeGreaterThanOrEqual(3);
    }
    const dark = solidColor(named(".topbar :where(button, a, select, input):focus-visible, .test-header :focus-visible, .site-footer :focus-visible")["outline-color"])!;
    expect(contrastRatio(dark, "#18201f")).toBeGreaterThanOrEqual(3);
  });

  it("does not remove focus outlines without a replacement", () => {
    const removed = rules.filter((r) => /outline:\s*(none|0)\b/.test(`outline: ${r.declarations.outline ?? ""}`) && /focus-visible/.test(r.selector));
    // .demo-users replaces the outline with a strong border; anything else needs a reason here.
    expect(removed.map((r) => r.selector)).toEqual([".demo-users button:hover:not(:disabled), .demo-users button:focus-visible"]);
  });
});
