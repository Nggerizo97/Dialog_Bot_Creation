import { useState } from "react";
import { Bot } from "lucide-react";
import { devSignIn, type Session } from "./api";

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

export function Login({
  onSignedIn,
  signIn = devSignIn,
}: {
  onSignedIn: (session: Session) => void;
  signIn?: (subject: string, groups: string[]) => Promise<string>;
}) {
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");

  const choose = async (user: DemoUser) => {
    setBusy(user.subject);
    setError("");
    try {
      const token = await signIn(user.subject, user.groups);
      onSignedIn({ token, subject: user.subject });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy("");
    }
  };

  return (
    <main className="login-page">
      <section className="login-card">
        <div className="brand">
          <Bot size={22} strokeWidth={2.4} /> Bot_Dialog_Generator <span>Studio</span>
        </div>
        <h1>Sign in</h1>
        <p>
          Development sign-in. Pick a demo user: each one sees only the workspaces they belong to. In production you
          sign in with your company account.
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
        {error && (
          <p role="alert" className="login-error">
            {error}
          </p>
        )}
      </section>
    </main>
  );
}
