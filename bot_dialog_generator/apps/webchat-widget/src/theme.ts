// Colors of the chat widget, and the pairs that are used together.
//
// Every pair below is checked by theme.test.ts against WCAG 2.1 AA: 4.5:1 for text
// (1.4.3) and 3:1 for the borders and focus rings that identify a control (1.4.11).
// Change a color here, not in the component's CSS, so the check keeps covering it.

export const THEME = {
  ink: "#18201f",
  inkMuted: "#3f4b47",
  surface: "#fffef9",
  surfaceMuted: "#f0f4f0",
  surfaceNote: "#e3e9e3",
  header: "#18201f",
  onHeader: "#fffef9",
  accent: "#15766b",
  onAccent: "#ffffff",
  accentText: "#17594f",
  accentTint: "#e6f2ed",
  accentTintHover: "#d0e7de",
  /** Borders of inputs, checkboxes and buttons: what shows where a control is. */
  controlBorder: "#6a7670",
  /** Dividers and message bubbles: decoration, not needed to find or use anything. */
  divider: "#d5ddd5",
  danger: "#a8321f",
  dangerTint: "#fdf0ed",
  focusOnLight: "#15766b",
  focusOnDark: "#a8d8ce",
} as const;

export type ThemeColor = keyof typeof THEME;

export interface ContrastPair {
  foreground: ThemeColor;
  background: ThemeColor;
  /** "text" needs 4.5:1; "ui" (borders, focus rings, icons) needs 3:1. */
  kind: "text" | "ui";
  usedFor: string;
}

export const CONTRAST_PAIRS: ContrastPair[] = [
  { foreground: "ink", background: "surface", kind: "text", usedFor: "assistant messages, form labels, input text" },
  { foreground: "ink", background: "surfaceMuted", kind: "text", usedFor: "notices in the conversation" },
  { foreground: "inkMuted", background: "surfaceMuted", kind: "text", usedFor: "typing indicator" },
  { foreground: "inkMuted", background: "surfaceNote", kind: "text", usedFor: "automated-assistant notice" },
  { foreground: "inkMuted", background: "surface", kind: "text", usedFor: "help text, footer links" },
  { foreground: "accentText", background: "surfaceNote", kind: "text", usedFor: "links in the notice" },
  { foreground: "accentText", background: "surface", kind: "text", usedFor: "links, secondary buttons" },
  { foreground: "accentText", background: "accentTint", kind: "text", usedFor: "menu options" },
  { foreground: "accentText", background: "accentTintHover", kind: "text", usedFor: "menu options, hovered" },
  { foreground: "onAccent", background: "accent", kind: "text", usedFor: "the person's messages, launcher icon" },
  { foreground: "onHeader", background: "header", kind: "text", usedFor: "title and header buttons" },
  { foreground: "danger", background: "surface", kind: "text", usedFor: "error messages" },
  { foreground: "danger", background: "dangerTint", kind: "text", usedFor: "error notices in the conversation" },
  { foreground: "controlBorder", background: "surface", kind: "ui", usedFor: "input, checkbox and button borders" },
  { foreground: "controlBorder", background: "accentTint", kind: "ui", usedFor: "menu option borders" },
  { foreground: "focusOnLight", background: "surface", kind: "ui", usedFor: "focus ring" },
  { foreground: "focusOnLight", background: "surfaceMuted", kind: "ui", usedFor: "focus ring in the conversation" },
  { foreground: "focusOnDark", background: "header", kind: "ui", usedFor: "focus ring in the header" },
];

function channel(value: number): number {
  const c = value / 255;
  return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
}

/** Relative luminance of a #rrggbb color (WCAG 2.1). */
export function luminance(hex: string): number {
  const n = parseInt(hex.slice(1), 16);
  return 0.2126 * channel((n >> 16) & 255) + 0.7152 * channel((n >> 8) & 255) + 0.0722 * channel(n & 255);
}

/** Contrast ratio between two #rrggbb colors, from 1 to 21. */
export function contrastRatio(a: string, b: string): number {
  const [light, dark] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (light + 0.05) / (dark + 0.05);
}
