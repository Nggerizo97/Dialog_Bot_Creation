// The studio's privacy choices: what the person allowed in the privacy banner, and the
// only code that may send usage statistics. docs/legal/COOKIES_AND_STORAGE.md lists the key.

export const PRIVACY_CHOICES_KEY = "bdg.privacy_choices";
/** Raise when the categories change: everyone is asked again. */
export const CHOICES_VERSION = 1;
/** Ask again after a year, as consent should not be assumed to last forever. */
export const CHOICES_MAX_AGE_MS = 365 * 24 * 60 * 60 * 1000;

export interface PrivacyChoices {
  version: number;
  analytics: boolean;
  decided_at: string;
}

/** The saved choices, or null when the person has not decided (or must decide again). */
export function loadChoices(now: Date = new Date()): PrivacyChoices | null {
  try {
    const raw = localStorage.getItem(PRIVACY_CHOICES_KEY);
    if (!raw) return null;
    const saved = JSON.parse(raw) as PrivacyChoices;
    const age = now.getTime() - new Date(saved.decided_at).getTime();
    if (saved.version !== CHOICES_VERSION || !(age >= 0 && age < CHOICES_MAX_AGE_MS)) return null;
    return { version: saved.version, analytics: saved.analytics === true, decided_at: saved.decided_at };
  } catch {
    return null;
  }
}

export function saveChoices(analytics: boolean, now: Date = new Date()): PrivacyChoices {
  const choices: PrivacyChoices = { version: CHOICES_VERSION, analytics, decided_at: now.toISOString() };
  try {
    localStorage.setItem(PRIVACY_CHOICES_KEY, JSON.stringify(choices));
  } catch {
    // Storage blocked: the choice lasts until the page reloads.
  }
  return choices;
}

export type StudioEvent = "tab_viewed" | "draft_saved" | "bot_published" | "test_chat_opened";

/**
 * Counts a studio event, only when analytics is configured and the person allowed it.
 * Sends the event name and an allow-listed property: never who did it, what they
 * wrote, or which bot or workspace it was.
 */
export function track(analyticsUrl: string | undefined, choices: PrivacyChoices | null, event: StudioEvent, props: { tab?: string } = {}) {
  if (!analyticsUrl || choices?.analytics !== true) return;
  const body = JSON.stringify({ source: "studio", event, consent: { analytics: true, campaigns: false }, props });
  fetch(`${analyticsUrl.replace(/\/$/, "")}/events`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body,
    keepalive: true,
  }).catch(() => {
    // Statistics are optional; the studio does not depend on them.
  });
}
