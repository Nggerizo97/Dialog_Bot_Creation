// The legal documents in docs/legal, and the business details that fill them in.
import privacy from "../../../docs/legal/PRIVACY_POLICY.md?raw";
import terms from "../../../docs/legal/TERMS_OF_USE.md?raw";
import refunds from "../../../docs/legal/REFUND_POLICY.md?raw";
import cookies from "../../../docs/legal/COOKIES_AND_STORAGE.md?raw";
import business from "../../../docs/legal/BUSINESS_INFORMATION.md?raw";
import thirdParties from "../../../docs/legal/THIRD_PARTY_INVENTORY.md?raw";
import licenses from "../../../docs/legal/LICENSES_AND_ASSETS.md?raw";

export interface LegalDocument {
  slug: string;
  file: string;
  title: string;
  source: string;
}

/** In the order the footer lists them. */
export const LEGAL_DOCUMENTS: LegalDocument[] = [
  { slug: "privacy", file: "PRIVACY_POLICY.md", title: "Privacy Policy", source: privacy },
  { slug: "terms", file: "TERMS_OF_USE.md", title: "Terms of Use", source: terms },
  { slug: "refunds", file: "REFUND_POLICY.md", title: "Refund Policy", source: refunds },
  { slug: "cookies", file: "COOKIES_AND_STORAGE.md", title: "Cookies and storage", source: cookies },
  { slug: "business", file: "BUSINESS_INFORMATION.md", title: "Business and legal contact", source: business },
  { slug: "third-parties", file: "THIRD_PARTY_INVENTORY.md", title: "Third parties", source: thirdParties },
  { slug: "licenses", file: "LICENSES_AND_ASSETS.md", title: "Licenses", source: licenses },
];

export const legalHref = (slug: string) => `#/legal/${slug}`;

/** The document a #/legal/<slug> address points at, or null. */
export function legalDocumentForHash(hash: string): LegalDocument | null | undefined {
  const match = /^#\/legal\/([a-z-]+)$/.exec(hash);
  if (!match) return undefined; // not a legal address
  return LEGAL_DOCUMENTS.find((d) => d.slug === match[1]) ?? null;
}

/** Turns a link between documents ("TERMS_OF_USE.md") into its studio address. */
export function legalHrefForFile(file: string): string | null {
  const doc = LEGAL_DOCUMENTS.find((d) => d.file === file);
  return doc ? legalHref(doc.slug) : null;
}

export interface BusinessInfo {
  entityName: string;
  taxId: string;
  address: string;
  supportEmail: string;
  privacyEmail: string;
  dpoContact: string;
  termsVersion: string;
}

const PLACEHOLDERS: Record<keyof BusinessInfo, string> = {
  entityName: "[[COMPANY_LEGAL_NAME]]",
  taxId: "[[TAX_ID]]",
  address: "[[REGISTERED_ADDRESS]]",
  supportEmail: "[[SUPPORT_EMAIL]]",
  privacyEmail: "[[PRIVACY_EMAIL]]",
  dpoContact: "[[DPO_CONTACT]]",
  termsVersion: "[[TERMS_VERSION]]",
};

/**
 * Business details from the build environment. A value that is not configured stays
 * as its placeholder, so the studio never shows an invented name or number.
 */
export function businessInfo(env: ImportMetaEnv = import.meta.env): BusinessInfo {
  const value = (v: string | undefined, key: keyof BusinessInfo) => v?.trim() || PLACEHOLDERS[key];
  return {
    entityName: value(env.VITE_LEGAL_ENTITY_NAME, "entityName"),
    taxId: value(env.VITE_LEGAL_TAX_ID, "taxId"),
    address: value(env.VITE_LEGAL_ADDRESS, "address"),
    supportEmail: value(env.VITE_LEGAL_SUPPORT_EMAIL, "supportEmail"),
    privacyEmail: value(env.VITE_LEGAL_PRIVACY_EMAIL, "privacyEmail"),
    dpoContact: value(env.VITE_LEGAL_DPO_CONTACT, "dpoContact"),
    termsVersion: env.VITE_LEGAL_TERMS_VERSION?.trim() || "draft",
  };
}

export const isPlaceholder = (value: string) => /^\[\[.*\]\]$/.test(value);

/** Fills the placeholders the business details answer; the rest stay visible. */
export function fillPlaceholders(markdown: string, info: BusinessInfo): string {
  let out = markdown;
  for (const key of Object.keys(PLACEHOLDERS) as Array<keyof BusinessInfo>) {
    if (key === "termsVersion" || isPlaceholder(info[key])) continue;
    out = out.split(PLACEHOLDERS[key]).join(info[key]);
  }
  if (info.termsVersion !== "draft") {
    out = out.split("[[TERMS_VERSION]]").join(info.termsVersion).split("[[POLICY_VERSION]]").join(info.termsVersion);
  }
  return out;
}
