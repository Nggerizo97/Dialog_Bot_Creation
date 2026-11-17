import { useState } from "react";
import { Dialog } from "./Dialog";
import { legalHref } from "./legal";
import type { PrivacyChoices } from "./privacyChoices";

/**
 * Asks once whether the studio may count anonymous usage statistics. It does not
 * block the page, and "Reject non-essential" is the same kind of button, in the same
 * place and as many clicks away, as "Accept all".
 */
export function PrivacyBanner({ onDecide, onCustomize }: { onDecide: (analytics: boolean) => void; onCustomize: () => void }) {
  return (
    <section className="privacy-banner" role="region" aria-label="Privacy choices">
      <p>
        The studio uses storage that is <strong>strictly necessary</strong> to keep you signed in; it is always on. With your permission, it
        also counts <strong>anonymous usage statistics</strong>, such as which tabs are opened. Statistics never include who you are or what
        you write, and they stay off unless you accept. <a href={legalHref("cookies")}>Cookies and storage</a>
      </p>
      <div className="banner-actions">
        <button type="button" className="choice-button" onClick={() => onDecide(true)}>
          Accept all
        </button>
        <button type="button" className="choice-button" onClick={() => onDecide(false)}>
          Reject non-essential
        </button>
        <button type="button" className="choice-button" onClick={onCustomize}>
          Customize
        </button>
      </div>
    </section>
  );
}

/** The categories one by one. Opened from the banner or the footer's "Privacy choices". */
export function PrivacyChoicesDialog({
  current,
  onSave,
  onClose,
}: {
  current: PrivacyChoices | null;
  onSave: (analytics: boolean) => void;
  onClose: () => void;
}) {
  const [analytics, setAnalytics] = useState(current?.analytics === true);
  return (
    <Dialog title="Privacy choices" onClose={onClose} description={<p>Choose what the studio may keep. You can change this at any time from the footer.</p>}>
      <form
        className="dialog-form"
        onSubmit={(e) => {
          e.preventDefault();
          onSave(analytics);
        }}
      >
        <fieldset>
          <legend>Strictly necessary</legend>
          <p className="field-help">
            Keeps you signed in for this browser tab and remembers these choices. The studio cannot work without it, so it cannot be turned
            off.
          </p>
        </fieldset>
        <fieldset>
          <legend>Usage statistics</legend>
          <label className="check-label">
            <input type="checkbox" name="analytics" checked={analytics} onChange={(e) => setAnalytics(e.target.checked)} />
            <span>Count which studio features are used, anonymously. Never includes who you are or what you write.</span>
          </label>
        </fieldset>
        <div className="dialog-actions">
          <button type="submit" className="choice-button">
            Save choices
          </button>
          <button type="button" className="choice-button" onClick={() => onSave(false)}>
            Reject non-essential
          </button>
        </div>
      </form>
    </Dialog>
  );
}
