/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_STUDIO_API_URL?: string;
  /** Company sign-in (OIDC). Leave unset to use development sign-in. See docs/entra-setup.md. */
  readonly VITE_OIDC_AUTHORITY?: string;
  readonly VITE_OIDC_CLIENT_ID?: string;
  readonly VITE_OIDC_SCOPE?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
