import { useRef, useState } from "react";
import { Bot } from "lucide-react";
import { devSignIn, setPendingTerms, type Session } from "./api";
import { businessInfo, legalHref } from "./legal";

interface DemoUser {
  subject: string;
  groups: string[];
  label: string;
  detail: string;
}

// Matches the seeded store in services/studio-api (see the README).
export const DEMO_USERS: DemoUser[] = [
  { subject: "alice", groups: [], label: "Alice", detail: "Owner · Customer service" },
  { subject: "erin", groups: [], label: "Erin", detail: "Editor · Customer service" },
  { subject: "carol", groups: [], label: "Carol", detail: "Analyst · Customer service" },
  { subject: "bob", groups: [], label: "Bob", detail: "Owner · Human resources" },
  { subject: "frank", groups: ["hr-team"], label: "Frank", detail: "Editor · Human resources, through the hr-team group" },
  { subject: "dana", groups: ["bdg-platform-admins"], label: "Dana", detail: "Platform admin · sees every workspace" },
];

const TERMS_REQUIRED = "Tick the box to accept the Terms of Use and the Privacy Policy before signing in.";

export function Login({
  onSignedIn,
  signIn = devSignIn,
  startCompanySignIn,
  initialError = "",
  termsVersion = businessInfo().termsVersion,
}: {
  onSignedIn: (session: Session) => void;
  signIn?: (subject: string, groups: string[]) => Promise<string>;
  /** Set when company sign-in (Entra ID) is configured; replaces the demo users. */
  startCompanySignIn?: () => Promise<void>;
  initialError?: string;
  termsVersion?: string;
}) {
  const [busy, setBusy] = useState("");
  const [error, setError] = useState(initialError);
  const [accepted, setAccepted] = useState(false);
  const termsBox = useRef<HTMLInputElement>(null);

  /** Signing in needs the terms accepted; the acceptance is recorded after sign-in. */
  const requireTerms = () => {
    if (accepted) {
      setPendingTerms(termsVersion);
      return true;
    }
    setError(TERMS_REQUIRED);
    termsBox.current?.focus();
    return false;
  };

  const company = async () => {
    if (!requireTerms()) return;
    setBusy("company");
    setError("");
    try {
      await startCompanySignIn?.();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setBusy("");
    }
  };

  const choose = async (user: DemoUser) => {
    if (!requireTerms()) return;
    setBusy(user.subject);
    setError("");
    try {
      const token = await signIn(user.subject, user.groups);
      onSignedIn({ token, subject: user.subject });
    } catch (e) {
      setPendingTerms(null);
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy("");
    }
  };

  return (
    <main id="main-content" className="login-page">
      <section className="login-card" aria-labelledby="sign-in-title">
        <div className="brand">
          <Bot size={22} strokeWidth={2.4} aria-hidden="true" /> Bot_Dialog_Generator <span>Studio</span>
        </div>
        <h1 id="sign-in-title">Sign in</h1>
        <label className="check-label terms-check">
          <input
            ref={termsBox}
            type="checkbox"
            checked={accepted}
            onChange={(e) => {
              setAccepted(e.target.checked);
              if (e.target.checked && error === TERMS_REQUIRED) setError("");
            }}
            aria-describedby="terms-help"
          />
          <span>
            I have read and accept the{" "}
            <a href={legalHref("terms")} target="_blank" rel="noopener noreferrer">
              Terms of Use<span className="sr-only"> (opens in a new tab)</span>
            </a>{" "}
            and the{" "}
            <a href={legalHref("privacy")} target="_blank" rel="noopener noreferrer">
              Privacy Policy<span className="sr-only"> (opens in a new tab)</span>
            </a>
            .
          </span>
        </label>
        <p id="terms-help" className="field-help">
          Required. The studio records the version you accepted.
        </p>
        {startCompanySignIn ? (
          <>
            <p>Sign in with your work account. You will see only the areas your groups give you access to.</p>
            <button className="company-sign-in" onClick={company} disabled={busy !== ""}>
              {busy === "company" ? "Opening Microsoft sign-in…" : "Sign in with Microsoft"}
            </button>
          </>
        ) : (
          <>
            <p>
              Development sign-in. Pick a demo user: each one sees only the workspaces they belong to. In production you sign in with your
              company account.
            </p>
            <ul className="demo-users">
              {DEMO_USERS.map((user) => (
                <li key={user.subject}>
                  <button onClick={() => choose(user)} disabled={busy !== ""}>
                    <strong>{busy === user.subject ? `Signing in as ${user.label}…` : user.label}</strong>
                    <small>{user.detail}</small>
                  </button>
                </li>
              ))}
            </ul>
          </>
        )}
        {error && (
          <p role="alert" className="login-error">
            {error}
          </p>
        )}
      </section>
    </main>
  );
}
