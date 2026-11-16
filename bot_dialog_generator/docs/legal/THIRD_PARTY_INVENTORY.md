# Third-party software and services

What the platform sends to, or loads from, anyone other than [[COMPANY_LEGAL_NAME]]. Audited on 16 Nov 2026 against `apps/studio-web/package.json`, `apps/webchat-widget/package.json`, `libs/ts/contracts/package.json`, `go.mod`, and the URLs in the production bundles (`grep -ohE "https?://…" dist/assets/*.js`).

## Result

**No trackers, analytics SDKs, advertising pixels, session recorders, chat or CRM scripts, tag managers, CDN-hosted scripts or CDN-hosted fonts.** The only absolute URLs in the studio bundle are XML namespace identifiers (`http://www.w3.org/…`, never fetched), React's error-decoder link (shown only in development error messages) and `localhost` defaults.

## Network calls the browser makes

| From | To | When | What is sent |
|---|---|---|---|
| Studio | studio-api (ours) | Every action | The signed-in user's bearer token and the action |
| Studio | Microsoft Entra ID (`login.microsoftonline.com`) | Sign-in and token renewal, only when `VITE_OIDC_*` is set | The standard OpenID Connect sign-in exchange |
| Studio | analytics service (ours, `VITE_ANALYTICS_URL`) | Only after the person allows analytics in the banner | Event name and tab name; no user ID, no content |
| Chat widget | channel gateway (ours, `gateway-url`) | Messages, consent proof, deletion requests | Random conversation ID, message text, consent flags, allow-listed campaign tags when opted in |
| Chat widget | analytics service (ours, `analytics-url`) | Only after the person allows it | Event name, channel, allow-listed campaign tags when opted in; never text or IDs |

## Service-to-service calls

| From | To | What is sent |
|---|---|---|
| channel-gateway | studio-api `/internal/…` | Consent proof and "delete my data" requests, authenticated with `INTERNAL_API_TOKEN`; the workspace comes from the gateway's binding |
| campaigns | SMTP relay (`SMTP_ADDR`) | The rendered email, to the recipient only |
| studio-api, campaigns | PostgreSQL | Under row-level security, as an application role that owns no tables |

## Planned (not yet integrated)

| Provider | For | Before it is switched on |
|---|---|---|
| [[AI_PROVIDER_AND_REGION]] (proposed: Claude on Amazon Bedrock) | AI answers (milestone 5) | Data processing terms with "no training on customer data"; region decided; listed in the Privacy Policy |
| Hosting, [[HOSTING_PROVIDER_AND_REGION]] | Running the services | Data processing agreement; region in the Privacy Policy |
| Contact-center platform | Handoff to a person | Not chosen yet |

## Rules for adding a dependency

1. No script, font, image or style may be loaded from a third-party origin at runtime. Bundle it.
2. A new runtime package needs its license in [LICENSES_AND_ASSETS.md](LICENSES_AND_ASSETS.md) and, if it sends data anywhere, a row in this file and in the Privacy Policy.
3. Any new storage key needs a row in [COOKIES_AND_STORAGE.md](COOKIES_AND_STORAGE.md); the app tests fail on unlisted keys.
4. Anything that sets non-essential cookies or storage, or sends personal data to a third party, must sit behind the consent banner and default to off.
