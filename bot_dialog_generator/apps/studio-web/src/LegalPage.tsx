import { useEffect, useRef } from "react";
import { Bot } from "lucide-react";
import { Markdown } from "./Markdown";
import { LEGAL_DOCUMENTS, businessInfo, fillPlaceholders, legalHref, type BusinessInfo, type LegalDocument } from "./legal";

/** A legal document, readable without signing in, so the chat widget can link to it. */
export function LegalPage({ doc, info = businessInfo() }: { doc: LegalDocument | null; info?: BusinessInfo }) {
  const heading = useRef<HTMLDivElement>(null);
  useEffect(() => {
    // A new page: move focus to its start, like a page load would, and update the tab title.
    document.title = `${doc?.title ?? "Page not found"} · Bot_Dialog_Generator`;
    heading.current?.focus();
    window.scrollTo?.(0, 0);
  }, [doc]);

  return (
    <main id="main-content" className="legal-page">
      <nav className="legal-nav" aria-label="Legal documents">
        <a className="brand" href="#/">
          <Bot size={20} strokeWidth={2.4} aria-hidden="true" /> Bot_Dialog_Generator <span>Studio</span>
        </a>
        <ul>
          {LEGAL_DOCUMENTS.map((d) => (
            <li key={d.slug}>
              <a href={legalHref(d.slug)} aria-current={d.slug === doc?.slug ? "page" : undefined}>
                {d.title}
              </a>
            </li>
          ))}
        </ul>
      </nav>
      <article className="legal-article" aria-label={doc?.title ?? "Page not found"}>
        <div ref={heading} tabIndex={-1} className="legal-start">
          {doc ? (
            <>
              <p className="legal-status" role="note">
                Draft for legal review. Values in highlighted brackets are still to be completed.
              </p>
              <Markdown source={fillPlaceholders(doc.source, info)} />
            </>
          ) : (
            <>
              <h1>Page not found</h1>
              <p>
                Choose a document from the list, or go <a href="#/">back to the studio</a>.
              </p>
            </>
          )}
        </div>
      </article>
    </main>
  );
}
