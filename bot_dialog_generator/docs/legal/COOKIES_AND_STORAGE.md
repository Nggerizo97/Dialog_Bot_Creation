<!--
DRAFT TEMPLATE. Not legal advice. This list must match the code: each key below is
defined in apps/studio-web/src/privacyChoices.ts, apps/studio-web/src/api.ts,
apps/studio-web/src/companySignIn.ts (oidc-client-ts) or
apps/webchat-widget/src/bot-dialog-generator-chat.ts (STORAGE_KEYS), and the tests
in both apps fail if an unlisted bot_dialog_generator or bdg. key appears.
Checked against the code on 16 Nov 2026.
-->

# Cookies and browser storage

**Effective date:** [[EFFECTIVE_DATE]]

**Bot_Dialog_Generator sets no cookies.** It does not load advertising, social-media, session-recording or third-party analytics scripts, and it loads no fonts or code from other companies' servers. The values below are all it keeps in your browser.

## Categories

| Category | Default | What it covers |
|---|---|---|
| **Strictly necessary** | Always on | What the service needs to work for you: your sign-in in the studio, the conversation you started in the chat, and the record of your privacy choices. It cannot be switched off, because the service would not work, and it needs no consent (ePrivacy Directive, Art. 5(3)). |
| **Analytics** | **Off** until you allow it | Anonymous counts of events, such as "a tab was opened" or "a message was sent". Never what you wrote, your conversation ID or your identity. |
| **Campaigns** | **Off** until you allow it | Which campaign link brought you to the chat (`utm_source`, `utm_medium`, `utm_campaign`), added to the statistics above. |

Analytics and campaign data are **not stored in your browser at all**: when you allow them, events are sent to our own analytics service, which keeps only totals. When you have not allowed them, nothing is sent; the analytics service also refuses any event that does not carry your opt-in.

## Studio (staff)

| Key | Storage | What it holds | Lifetime | Category |
|---|---|---|---|---|
| `oidc.user:<authority>:<client-id>` | sessionStorage | Your Microsoft Entra ID sign-in (access, ID and refresh tokens), managed by `oidc-client-ts` | Until you sign out or close the tab | Strictly necessary |
| `oidc.<random state>` | sessionStorage | Short-lived values that protect the sign-in redirect (state, PKCE verifier) | Removed when sign-in finishes | Strictly necessary |
| `bdg.session` | sessionStorage | Development sign-in only (local environments): the demo token | Until you sign out or close the tab | Strictly necessary |
| `bdg.terms_pending` | sessionStorage | The version of the Terms and Privacy Policy you accepted on the sign-in page, until the studio records it | Removed right after sign-in | Strictly necessary |
| `bdg.privacy_choices` | localStorage | Your answer in the privacy banner: `{ "version": 1, "analytics": false, "decided_at": "…" }` | 12 months, then you are asked again; also when the categories change | Strictly necessary (it is the record of your choice) |

The privacy banner appears only when the studio is configured to collect usage statistics (`VITE_ANALYTICS_URL`). Without it the studio has nothing optional to ask about. You can open **Privacy choices** from the footer at any time.

## Chat widget (people who chat)

| Key | Storage | What it holds | Lifetime | Category |
|---|---|---|---|---|
| `bot_dialog_generator_chat_user_id` | sessionStorage | Random conversation ID, e.g. `usr-4f9a1c7e2b…` | Until you close the tab, end the chat or delete your data | Strictly necessary |
| `bot_dialog_generator_messages_<id>` | sessionStorage | The conversation shown on screen, so it survives a reload | Same | Strictly necessary |
| `bot_dialog_generator_consent` | sessionStorage | What you accepted before chatting and your optional choices | Same | Strictly necessary (it is the record of your choice) |

Earlier versions of the widget kept conversations in localStorage. The current widget deletes those entries the first time it loads.

The widget runs on other organizations' websites. Those websites may set their own cookies; this document covers only the widget.

## How to delete it

- **Chat:** **Chat options → End chat and clear it** removes all three keys at once; **Delete my data** also asks our servers to delete their copy. Closing the tab removes them too.
- **Studio:** **Sign out** removes the sign-in keys; **Privacy choices → Reject non-essential** keeps analytics off. You can also clear site data in your browser settings.

## Regulations considered

| Law | What it means here |
|---|---|
| EU/EEA ePrivacy Directive Art. 5(3), GDPR | Strictly necessary storage needs no consent but must be disclosed (this page). Analytics and campaign tracking need prior opt-in, which the banner and the chat prompt collect, with "reject" as easy as "accept". |
| UK PECR, UK GDPR | Same as the EU. |
| California CCPA/CPRA | No personal information is sold or shared for cross-context behavioral advertising, so no "Do Not Sell or Share" link is required. |
| Colombia, Ley 1581 de 2012 | No cookie-specific rule. Optional processing is covered by the authorization in the chat, which can be revoked. |
