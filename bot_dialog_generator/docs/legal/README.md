# Legal, privacy and accessibility pack

> **Status: draft templates. Not legal advice.** These documents describe the product's actual behavior, checked in the code on 16 Nov 2026, but a lawyer in the organization's jurisdiction must review them before anything is published. Replace every `[[PLACEHOLDER]]` first (list below).

## Documents

| Document | Who reads it | Where it is shown |
|---|---|---|
| [PRIVACY_POLICY.md](PRIVACY_POLICY.md) | People who chat, and staff | Studio footer and sign-in page; chat widget footer, menu and pre-chat step (`privacy-url`) |
| [TERMS_OF_USE.md](TERMS_OF_USE.md) | Staff (part 1) and people who chat (part 2) | Accepted on the studio sign-in page (recorded in the audit log); chat widget (`terms-url`) |
| [REFUND_POLICY.md](REFUND_POLICY.md) | Anyone | Studio footer. Today it states that nothing is charged |
| [COOKIES_AND_STORAGE.md](COOKIES_AND_STORAGE.md) | Anyone | Studio footer and privacy banner; chat widget (`cookies-url`) |
| [BUSINESS_INFORMATION.md](BUSINESS_INFORMATION.md) | Anyone | Studio footer (entity and tax ID) and "Business and legal contact" page |
| [THIRD_PARTY_INVENTORY.md](THIRD_PARTY_INVENTORY.md) | Privacy and security reviewers | Studio footer |
| [LICENSES_AND_ASSETS.md](LICENSES_AND_ASSETS.md) | Anyone | Studio footer |

The studio renders all of these from this folder at `#/legal/<page>` (for example `http://localhost:5173/#/legal/privacy`), with no sign-in, so the chat widget can link to them. In production, publish the reviewed versions at stable URLs and set them on each widget:

```html
<bot-dialog-generator-chat
  privacy-url="https://[[YOUR_DOMAIN]]/privacy"
  terms-url="https://[[YOUR_DOMAIN]]/terms"
  cookies-url="https://[[YOUR_DOMAIN]]/cookies"
  policy-version="[[POLICY_VERSION]]"
  require-authorization
></bot-dialog-generator-chat>
```

## How each requirement is met

| Requirement | Where it is implemented | Proved by |
|---|---|---|
| Workspace isolation for personal data | Migration `000002_privacy` puts consent, campaign and erasure tables under forced row-level security | `TestRowLevelSecurityOnPersonalData` (studio-api) |
| Consent banner: necessary on, analytics off until opt-in | Studio `PrivacyBanner`; chat widget optional prompt; analytics service refuses events without the opt-in | `privacy.test.tsx` "Privacy banner"; widget "Analytics and campaign opt-ins"; `TestEventsNeedTheAnalyticsOptIn` |
| "Reject" as easy as "Accept" | Same style, same number of clicks, side by side, in banner, chat prompt, pre-chat step and deletion confirmation | Class and order checks in both test suites |
| Unchecked, plain-language consent boxes linked to the policies | Studio sign-in (terms), publish-for-minors confirmation; widget pre-chat step | "starts with every box unchecked" tests |
| Proof of consent | `POST /channels/webchat/consent` → studio-api `subject_consents` (no IP, no browser data) | `TestConsentProofKeepsOnlyKnownPurposes` |
| Data minimization | Proto `InboundMessage` drops location and media; random 128-bit chat IDs required; gateway forwards only allow-listed campaign tags with opt-in; engine errors never echo user text; page events omit message text and IDs; analytics accepts only codes | Gateway, widget and analytics tests |
| Age gate and parental consent | Widget `min-age`, `audience="minors"`, `parental-consent="host"` + `grantParentalConsent()`; bot `audience`/`min_age` settings; publishing a bot for minors needs the owner's confirmation, audited | "Minors and parental consent" tests; `TestBotsForMinorsNeedParentalConsentToPublish` |
| Right to erasure, chat users | Widget **Delete my data** → gateway → studio-api; owners in the studio's Privacy tab | `TestEraseChatUserDeletesConsentProofAndQueuesTheRest`, `TestErasureStaysInsideTheWorkspace`, widget deletion tests |
| Right to erasure, campaign recipients | Studio Privacy tab, by email: deletes delivery records, adds a hashed suppression | `TestEraseEmailPurgesCampaignRecordsInOneWorkspaceOnly` |
| Right to erasure, staff | Studio Privacy tab → admin executes; never orphans an area | `TestStaffCanAskForTheirDataToBeDeleted`, `TestStaffErasureNeverLeavesAnAreaWithoutAnOwner` |
| Erasure covers every table | `tableDataClasses` must classify every table a migration creates | `TestEveryTableIsClassifiedForErasure` |
| Data held outside PostgreSQL (sessions, analytics, vectors) | Erasure writes a `data_subject.erasure_requested` outbox event for the service that owns it | Outbox assertions in the erasure tests |
| Email unsubscribe | `services/campaigns`: `List-Unsubscribe` + `List-Unsubscribe-Post` (RFC 8058), visible link in both bodies, signed tokens without the address, suppression checked right before each send, no send if the list is unreadable | `services/campaigns/campaigns_test.go` |
| No hidden fees, fake reviews or unverified claims | Demo replies labelled "Sample data"; no fake "connecting you to an agent"; widget never invents answers when offline; no testimonials or accuracy claims anywhere; Refund Policy states there are no charges | Widget "Honest answers"; gateway tests |
| Licensed fonts and icons | [LICENSES_AND_ASSETS.md](LICENSES_AND_ASSETS.md); font licenses shipped in `dist/licenses/` | Studio build |
| No third-party trackers | [THIRD_PARTY_INVENTORY.md](THIRD_PARTY_INVENTORY.md) | Bundle URL audit |
| WCAG 2.1 AA contrast | Widget colors in `theme.ts`; studio colors in `styles.css` | `theme.test.ts`; `contrast.test.ts` parses `styles.css` |
| Keyboard, focus and announcements | Focus trap and restore in the chat dialog, the studio test chat and studio dialogs; Escape closes; menu arrows; `:focus-visible` everywhere; `aria-live="polite"` logs; skip link; labelled icon buttons | Keyboard tests in both apps |

## Regulations that may apply

Which laws apply depends on where the organization is established and where the people chatting with its bots are. Fill in `[[JURISDICTION]]` and remove what does not apply.

| Law | Applies when | What it requires of this product |
|---|---|---|
| **Colombia, Ley 1581 de 2012**, Decreto 1377 de 2013 (Decreto 1074 de 2015) | The organization is in Colombia or processes data of people there | Prior, express and informed authorization (Art. 9) — the widget's `require-authorization` step, with proof kept; a published policy; rights to know, update, rectify, revoke and delete (Art. 8); queries in 10 business days, claims in 15; SIC database registration when thresholds are met; transfer rules (Art. 26). Children's data only in their best interest, with the guardian's authorization (Art. 7) |
| **Colombia, Ley 1480 de 2011** (consumer protection) | Anything sold to consumers | Full price disclosure, withdrawal right (Art. 47), payment reversal (Art. 51) — see REFUND_POLICY.md |
| **EU/EEA GDPR**, UK GDPR | Established there, or offering bots to people there | Transparency (Arts. 13–14), lawful basis (Art. 6), rights (Arts. 15–21), one-month answers (Art. 12), children's consent (Art. 8), records of processing, DPIA for large-scale monitoring, transfer safeguards |
| **ePrivacy Directive** Art. 5(3), UK PECR | Storing data on devices | Consent for non-essential storage; strictly necessary storage disclosed |
| **EU AI Act** Art. 50(1) | AI systems interacting with people in the EU, from 2 Aug 2026 | Tell people they are interacting with an AI system. **Done:** the widget says so before the first message |
| **California CCPA/CPRA** | A business meeting the thresholds, with California residents | Notice at collection, rights to know, delete and correct; no "Do Not Sell or Share" link needed while nothing is sold or shared |
| **CAN-SPAM**, Gmail/Yahoo bulk-sender rules, Colombian Ley 1581 | Campaign emails | Working unsubscribe honored promptly, sender's postal address, one-click `List-Unsubscribe` for bulk senders — all enforced by `services/campaigns` |
| **WCAG 2.1 AA** (EU Accessibility Act, ADA case law, Colombian Ley 1618 de 2013 and Resolución 1519 de 2020) | Public-facing chat widgets in particular | Covered by the tests above; still run an automated scanner and a screen-reader pass before launch |

## Risks to resolve before going live

| # | Risk | Severity | What to do |
|---|---|---|---|
| 1 | **Placeholders and legal review.** None of these documents can be published as they are | High | Fill the placeholders, have counsel review, translate into the users' language (Spanish for Colombia) |
| 2 | **AI provider and transfers** (milestone 5) | High | Choose the region; sign data processing terms that forbid training on prompts; list the provider in the Privacy Policy; check transfer rules |
| 3 | **Retention not decided.** Proposed periods are in the Privacy Policy | Medium | Decide them and add deletion jobs before transcripts are stored (milestones 3 and 8) |
| 4 | **Erasure events need consumers.** The outbox events are written, but the outbox relay and the engine, analytics and knowledge consumers do not exist yet | Medium | Build the relay (PLAN milestone 2) and an erasure handler in each service as it starts storing data; until then those stores hold nothing about people |
| 5 | **Requests by email or phone** need someone to verify identity before an owner erases in the studio | Medium | Name who answers, within which deadline, and how identity is checked; the studio records that check in the ledger |
| 6 | **Parental-consent process.** The platform asks for a guardian's permission and records it, but a checkbox does not verify who ticked it | Medium | For bots aimed at children, use `parental-consent="host"` with a verified process on the hosting site, or confirm with counsel that the checkbox is enough for the risk |
| 7 | **Break-glass transcript access** is decided but not built | Medium | Build it (reason, issue reference, time limit, audit) before transcripts are stored |
| 8 | **No LICENSE file in the repository** | Low | Decide: proprietary ("All rights reserved") or an open-source license |
| 9 | **Accessibility verified by unit tests only** | Low | Run axe or Lighthouse and NVDA/VoiceOver passes on the studio and the widget before launch |

## Handling a deletion request

1. **From the chat:** nothing to do; the widget's **Delete my data** runs the erasure and shows the person a reference.
2. **By email or phone:** check that the request comes from the person (for a chat, the website, date and time of the conversation; for campaigns, a reply from that mailbox). An owner of the area then opens **Privacy** in the studio, enters the conversation ID or email address and how identity was checked, and confirms. The ledger keeps only a one-way hash of the identifier.
3. **Staff:** the person clicks **Request deletion of my data** in the studio's Privacy tab; a platform admin runs it from the Admin page. If they are an area's last owner, that access is kept until someone else is made owner, and the request shows as partially completed.
4. Answer within the legal deadline (10 business days for queries and 15 for claims in Colombia; one month in the EU/UK).

## Placeholders

| Placeholder | Meaning |
|---|---|
| `[[COMPANY_LEGAL_NAME]]` | Registered name of the organization that operates the bots |
| `[[REGISTERED_ADDRESS]]` | Registered office address |
| `[[TAX_ID]]` | Tax or registration number (NIT in Colombia) |
| `[[PRIVACY_EMAIL]]`, `[[SUPPORT_EMAIL]]`, `[[PHONE]]` | Contact channels |
| `[[DPO_CONTACT]]` | Data protection officer or privacy lead |
| `[[SECURITY_CONTACT]]` | Where staff report security incidents |
| `[[JURISDICTION]]`, `[[CITY, COUNTRY]]` | Governing law and courts |
| `[[SUPERVISORY_AUTHORITY]]` | Data protection authority (SIC in Colombia) |
| `[[HOSTING_PROVIDER_AND_REGION]]`, `[[AI_PROVIDER_AND_REGION]]` | Processors and regions |
| `[[RETENTION_*]]` | Retention periods (risk 3); proposals are in the Privacy Policy |
| `[[EFFECTIVE_DATE]]`, `[[POLICY_VERSION]]`, `[[TERMS_VERSION]]` | Dates and versions; the versions must match `policy-version` on the widget and `VITE_LEGAL_TERMS_VERSION` in the studio |
| `[[YOUR_DOMAIN]]` | Where the documents are published |
| `[[AGE]]` | Minimum age for using the chat without a guardian |
| `[[WITHDRAWAL_DAYS]]`, `[[REFUND_DAYS]]`, `[[CANCELLATION_CHANNEL]]` | Only once something is sold |
| `[[REVIEW_TEAM]]`, `[[LIST COMPANY POLICIES ...]]` | Internal review team and policies the terms rely on |

Blocks starting with `[[OPTIONAL`, `[[CONFIRM` or `[[COMPLETE` are decisions, not just values: keep, change or delete them.
