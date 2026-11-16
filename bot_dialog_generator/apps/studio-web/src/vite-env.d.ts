/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_STUDIO_API_URL?: string;
  /** Company sign-in (OIDC). Leave unset to use development sign-in. See docs/entra-setup.md. */
  readonly VITE_OIDC_AUTHORITY?: string;
  readonly VITE_OIDC_CLIENT_ID?: string;
  readonly VITE_OIDC_SCOPE?: string;
  /** Analytics service. When set, the privacy banner asks for opt-in; nothing is sent before it. */
  readonly VITE_ANALYTICS_URL?: string;
  /** Business and legal contact shown in the footer (docs/legal/BUSINESS_INFORMATION.md). */
  readonly VITE_LEGAL_ENTITY_NAME?: string;
  readonly VITE_LEGAL_TAX_ID?: string;
  readonly VITE_LEGAL_ADDRESS?: string;
  readonly VITE_LEGAL_SUPPORT_EMAIL?: string;
  readonly VITE_LEGAL_PRIVACY_EMAIL?: string;
  readonly VITE_LEGAL_DPO_CONTACT?: string;
  /** Version of the Terms of Use and Privacy Policy accepted at sign-in. */
  readonly VITE_LEGAL_TERMS_VERSION?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
