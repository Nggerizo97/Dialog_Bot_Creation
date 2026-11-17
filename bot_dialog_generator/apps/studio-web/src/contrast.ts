// WCAG 2.1 contrast, used by contrast.test.ts to check styles.css.

function channel(value: number): number {
  const c = value / 255;
  return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
}

export function parseHex(hex: string): [number, number, number] {
  let h = hex.replace("#", "").toLowerCase();
  if (h.length === 3) h = [...h].map((c) => c + c).join("");
  const n = parseInt(h, 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

export function luminance(hex: string): number {
  const [r, g, b] = parseHex(hex);
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

export function contrastRatio(a: string, b: string): number {
  const [light, dark] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (light + 0.05) / (dark + 0.05);
}

export interface CssRule {
  selector: string;
  declarations: Record<string, string>;
}

/** Flat list of rules; rules inside @media keep the media query in their selector. */
export function parseRules(css: string): CssRule[] {
  const rules: CssRule[] = [];
  const clean = css.replace(/\/\*[\s\S]*?\*\//g, "");
  for (const match of clean.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const declarations: Record<string, string> = {};
    for (const part of match[2].split(";")) {
      const [name, ...value] = part.split(":");
      if (name.trim() && value.length) declarations[name.trim()] = value.join(":").trim();
    }
    rules.push({ selector: match[1].trim(), declarations });
  }
  return rules;
}

const HEX = /#(?:[0-9a-f]{6}|[0-9a-f]{3})\b/i;

/** The solid color of a declaration value, if it is one. */
export function solidColor(value: string | undefined): string | null {
  if (!value) return null;
  const match = HEX.exec(value);
  return match && !/gradient|url\(/i.test(value) ? match[0] : null;
}
