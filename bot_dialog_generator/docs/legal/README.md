# Legal and compliance pack

> **Status: draft templates. Not legal advice.** These documents were written from the product's actual behavior (checked in the code on 7 Oct 2026), but they must be reviewed by a lawyer in the organization's jurisdiction before anything is published. Replace every `[[PLACEHOLDER]]` first.

| Document | Who reads it | Where it goes |
|---|---|---|
| [privacy-notice-chat.md](privacy-notice-chat.md) | People who chat with a bot | Linked from the chat widget (`privacy-url` attribute) |
| [privacy-notice-staff.md](privacy-notice-staff.md) | Staff who use the studio | Linked from the studio sign-in page and footer |
| [terms-of-use.md](terms-of-use.md) | Staff who use the studio | Accepted once at first sign-in |

## What does not apply yet

The original compliance checklist was written for public commercial websites. These items have no counterpart in the product today:

| Item | Why it does not apply | Revisit when |
|---|---|---|
| Refund and cancellation policy | Nothing is sold; there are no payments or orders | The platform is sold to other companies |
| Testimonials and performance claims | There are no reviews or marketing claims | A public marketing site exists |
| `LocalBusiness` / `Organization` schema markup | For search engines on public sites; the studio is behind sign-in | A public marketing site exists |
| Lead capture forms and marketing consent | There are no lead forms or newsletters | Same |
| Image alt text | The apps contain no images; icons are decorative and hidden from screen readers by lucide | Images are added |

## Cookies and browser storage

The product sets **no cookies** and loads **no analytics, advertising or other third-party scripts** (fonts are bundled since commit `a2933b0`).

| Where | Key | What it holds | Lifetime | Category |
|---|---|---|---|---|
| Chat widget | `bot_dialog_generator_chat_user_id` (sessionStorage) | Random conversation ID, e.g. `usr-k3x9q2a` | Until the tab closes | Strictly necessary |
| Chat widget | `bot_dialog_generator_messages_<id>` (sessionStorage) | The visible conversation, so it survives a page reload | Until the tab closes | Strictly necessary |
| Studio | `bdg.session` (sessionStorage) | Development sign-in token. Replaced by Entra ID sign-in, whose tokens stay in memory | Until the tab closes | Strictly necessary |

**Is a consent banner required? No, not with the product as it is today.**

- **EU/EEA and UK (ePrivacy Directive Art. 5(3), GDPR):** storing information on a device needs consent unless it is strictly necessary for a service the user explicitly asked for. Keeping the conversation the user started, for that tab only, meets that exception. It must be disclosed, which the chat privacy notice does.
- **California (CCPA/CPRA):** there is no banner requirement. A "Do Not Sell or Share My Personal Information" link is needed only if personal information is sold or shared for cross-context behavioral advertising, which the product does not do.
- **Colombia (Ley 1581 de 2012):** there is no cookie-specific rule, but see the authorization requirement below.

**This changes as soon as** analytics, session replay, advertising pixels or a third-party chat or CRM script is added. Then a prior-consent banner with these categories is needed in the EU/UK: *Strictly necessary* (always on), *Functional*, *Performance/analytics*, *Targeting/advertising* (all off until the user opts in). Planned analytics (milestone 8) are first-party and server-side, which does not need a banner, but the privacy notices must be updated.

## Regulations that may apply

Which laws apply depends on where the organization is established and where the people chatting with its bots are. Fill in `[[JURISDICTION]]` and remove what does not apply.

| Law | Applies when | What it requires of this product |
|---|---|---|
| **Colombia, Ley 1581 de 2012** and Decreto 1377 de 2013 (compiled in Decreto 1074 de 2015) | The organization is in Colombia or processes data of people in Colombia | **Prior, express and informed authorization** before processing personal data (Art. 9); a published data-processing policy (*Política de Tratamiento de la Información*); rights to know, update, rectify, revoke authorization and request deletion (Art. 8); answer queries within 10 business days and claims within 15 (Arts. 14–15); registration in the SIC's *Registro Nacional de Bases de Datos* when the organization meets the thresholds; international transfers only to countries with adequate protection or under an exception (Art. 26) |
| **EU/EEA GDPR** and UK GDPR | The organization is established there, or offers bots to people there | Transparency (Arts. 13–14), lawful basis (Art. 6), rights of access, rectification, erasure, restriction, portability and objection (Arts. 15–21), answers within one month (Art. 12), records of processing, data protection impact assessment for large-scale monitoring, transfer safeguards for data leaving the EEA |
| **EU AI Act**, Art. 50(1) | AI systems interacting with people in the EU, from 2 Aug 2026 | People must be told they are interacting with an AI system unless obvious. **Done:** the widget shows "You're chatting with an automated assistant, not a person." |
| **California CCPA/CPRA** | A for-profit business meeting the revenue or data-volume thresholds, with California residents (including employees) | Notice at collection, rights to know, delete, correct and limit use of sensitive data, no discrimination for exercising rights |
| Accessibility: **WCAG 2.1 AA** (referenced by the EU Accessibility Act, ADA case law, Colombian Ley 1618 de 2013 and Resolución 1519 de 2020 for public entities) | Public-facing chat widgets in particular | Covered for the issues found on 7 Oct 2026; re-check with an automated scanner and a screen reader before launch |

## Risks to resolve before going live

| # | Risk | Severity | What to do |
|---|---|---|---|
| 1 | **Colombian authorization.** If the organization is Colombian, a notice may not be enough: Ley 1581 asks for prior, express authorization before processing personal data, and people type personal data into chats | High | Ask counsel whether the bot's purpose falls under an exception (Art. 10). If not, show an authorization step in the widget before the first message, with a link to the policy, and keep proof of it |
| 2 | **AI model provider and transfers.** Milestone 5 sends conversation text to a model (proposed: Claude on Amazon Bedrock) | High | Choose the region; sign the provider's data processing terms; confirm prompts are not used for training; list the provider in the privacy notices; check transfer rules (Colombia Art. 26, GDPR Chapter V) |
| 3 | **Retention not defined.** Server-side conversation storage arrives with milestones 3 and 8 | Medium | Set retention periods per data type (for example 90 days for transcripts, 1 year for audit log) and implement deletion jobs before storing transcripts |
| 4 | **Rights requests need a process.** Nobody can yet find or delete one person's conversations | Medium | Add per-user lookup and deletion to the engine and analytics stores; name who answers requests and within which deadline |
| 5 | **Break-glass transcript access** is decided but not built | Medium | Build it as planned (reason, issue reference, time limit, audit) before transcripts are stored |
| 6 | **Bot content written by areas** may include personal data, wrong information or promises the organization cannot keep | Medium | The terms of use make authors responsible; add a review step before publishing customer-facing bots |
| 7 | **No LICENSE file in the repository** | Low | Decide: proprietary ("All rights reserved") or an open-source license. Without one, nobody else may legally reuse the code |
| 8 | **Accessibility is only partially verified** | Low | Run axe or Lighthouse and a screen-reader pass (NVDA, VoiceOver) on the studio and widget before launch |

## How to publish

1. Replace the placeholders in all three documents. The list is below.
2. Have counsel review them, and translate them into the language your users read (Spanish for Colombia).
3. Publish the chat notice at a stable URL, then set it on every widget:
   ```html
   <bot-dialog-generator-chat privacy-url="https://[[YOUR_DOMAIN]]/privacy/chat"></bot-dialog-generator-chat>
   ```
4. Link the staff notice and the terms of use from the studio sign-in page, and record acceptance at first sign-in (planned with Entra ID sign-in, milestone 1).
5. Re-check this file whenever a new data store, third-party service, analytics tool or AI provider is added.

## Placeholders

| Placeholder | Meaning |
|---|---|
| `[[COMPANY_LEGAL_NAME]]` | Registered name of the organization that operates the bots |
| `[[REGISTERED_ADDRESS]]` | Registered office address |
| `[[TAX_ID]]` | Tax or registration number (NIT in Colombia) |
| `[[PRIVACY_EMAIL]]` | Mailbox for privacy requests |
| `[[SUPPORT_EMAIL]]` | General support mailbox |
| `[[PHONE]]` | Contact phone number |
| `[[DPO_CONTACT]]` | Data protection officer or privacy lead, if required |
| `[[JURISDICTION]]` | Country and laws that govern the documents |
| `[[SUPERVISORY_AUTHORITY]]` | Data protection authority (SIC in Colombia; the national authority in the EU) |
| `[[HOSTING_PROVIDER_AND_REGION]]` | Where the platform runs, for example AWS in a named region |
| `[[AI_PROVIDER_AND_REGION]]` | AI model provider and region, once chosen |
| `[[RETENTION_*]]` | Retention periods, once decided (risk 3) |
| `[[EFFECTIVE_DATE]]` | Date the document takes effect |
| `[[YOUR_DOMAIN]]` | Domain where the notices are published |
| `[[AGE]]` | Minimum age for using the chat |
| `[[SECURITY_CONTACT]]` | Where staff report security incidents |
| `[[REVIEW_TEAM]]` | Team that reviews public-facing bots before first publication, if you adopt that step |
| `[[CITY, COUNTRY]]` | Courts with jurisdiction over the terms |
| `[[LIST COMPANY POLICIES ...]]` | Existing internal policies the terms rely on |

Blocks starting with `[[OPTIONAL` or `[[CONFIRM` are decisions, not just values: keep, change or delete them.
