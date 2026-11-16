import { describe, it, expect } from "vitest";
import { CONTRAST_PAIRS, THEME, contrastRatio } from "./theme";

describe("Widget colors (WCAG 2.1 AA)", () => {
  it("computes contrast like the WCAG definition", () => {
    expect(contrastRatio("#000000", "#ffffff")).toBeCloseTo(21, 5);
    expect(contrastRatio("#777777", "#ffffff")).toBeCloseTo(4.48, 2);
  });

  it.each(CONTRAST_PAIRS)("$foreground on $background is readable ($usedFor)", ({ foreground, background, kind }) => {
    const ratio = contrastRatio(THEME[foreground], THEME[background]);
    expect(ratio).toBeGreaterThanOrEqual(kind === "text" ? 4.5 : 3);
  });

  it("checks every color that carries text or marks a control", () => {
    const checked = new Set(CONTRAST_PAIRS.flatMap((p) => [p.foreground, p.background]));
    // The divider is decoration: nothing has to be read or found through it.
    const unchecked = Object.keys(THEME).filter((name) => !checked.has(name as keyof typeof THEME) && name !== "divider");
    expect(unchecked).toEqual([]);
  });
});
