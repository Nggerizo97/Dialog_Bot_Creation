import { LEGAL_DOCUMENTS, businessInfo, isPlaceholder, legalHref, type BusinessInfo } from "./legal";

/** Every page's footer: who runs the studio, how to reach them, and the legal documents. */
export function Footer({ info = businessInfo(), onPrivacyChoices }: { info?: BusinessInfo; onPrivacyChoices?: () => void }) {
  const value = (v: string) => (isPlaceholder(v) ? <mark className="placeholder">{v}</mark> : v);
  const email = (v: string) => (isPlaceholder(v) ? <mark className="placeholder">{v}</mark> : <a href={`mailto:${v}`}>{v}</a>);
  return (
    <footer className="site-footer">
      <nav aria-label="Legal">
        <ul>
          {LEGAL_DOCUMENTS.map((d) => (
            <li key={d.slug}>
              <a href={legalHref(d.slug)}>{d.title}</a>
            </li>
          ))}
          {onPrivacyChoices && (
            <li>
              <button type="button" className="link-button" onClick={onPrivacyChoices}>
                Privacy choices
              </button>
            </li>
          )}
        </ul>
      </nav>
      <address>
        <span>{value(info.entityName)}</span>
        <span>Tax ID {value(info.taxId)}</span>
        <span>{value(info.address)}</span>
        <span>Support {email(info.supportEmail)}</span>
        <span>Data Protection Officer {value(info.dpoContact)}</span>
      </address>
    </footer>
  );
}
