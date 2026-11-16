import { useEffect, useId, useRef, type ReactNode, type RefObject } from "react";

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/**
 * Keeps keyboard focus inside `ref` while it is shown: focus moves in when it opens,
 * Tab and Shift+Tab wrap around, Escape calls onEscape, and focus returns to where it
 * was when it closes.
 */
export function useFocusTrap(
  ref: RefObject<HTMLElement | null>,
  onEscape: () => void,
  { active = true, initialFocus }: { active?: boolean; initialFocus?: RefObject<HTMLElement | null> } = {},
) {
  const escape = useRef(onEscape);
  escape.current = onEscape;

  useEffect(() => {
    const container = ref.current;
    if (!active || !container) return;
    const previous = document.activeElement as HTMLElement | null;
    const focusable = () => [...container.querySelectorAll<HTMLElement>(FOCUSABLE)];
    (initialFocus?.current ?? focusable()[0] ?? container).focus();

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.stopPropagation();
        escape.current();
        return;
      }
      if (event.key !== "Tab") return;
      const items = focusable();
      if (items.length === 0) {
        event.preventDefault();
        return;
      }
      const first = items[0];
      const last = items[items.length - 1];
      if (event.shiftKey && (document.activeElement === first || !container.contains(document.activeElement))) {
        last.focus();
        event.preventDefault();
      } else if (!event.shiftKey && document.activeElement === last) {
        first.focus();
        event.preventDefault();
      }
    };
    container.addEventListener("keydown", onKeyDown);
    return () => {
      container.removeEventListener("keydown", onKeyDown);
      // Back to the control that opened it, if it is still on the page.
      if (previous && document.contains(previous)) previous.focus();
    };
  }, [ref, initialFocus, active]);
}

/** A modal dialog: named, described, focus kept inside, Escape closes. */
export function Dialog({
  title,
  description,
  onClose,
  children,
  alert = false,
  initialFocus,
}: {
  title: string;
  description?: ReactNode;
  onClose: () => void;
  children: ReactNode;
  /** For confirmations of something that cannot be undone. */
  alert?: boolean;
  initialFocus?: RefObject<HTMLElement | null>;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const descriptionId = useId();
  useFocusTrap(ref, onClose, { initialFocus });
  return (
    <div className="dialog-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div
        ref={ref}
        className="dialog"
        role={alert ? "alertdialog" : "dialog"}
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
        tabIndex={-1}
      >
        <h2 id={titleId}>{title}</h2>
        {description && (
          <div id={descriptionId} className="dialog-description">
            {description}
          </div>
        )}
        {children}
      </div>
    </div>
  );
}
