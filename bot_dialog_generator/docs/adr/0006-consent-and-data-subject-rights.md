# ADR 0006: Consent, data minimization and the right to erasure

## Status

Accepted. Implemented in studio-api (migration `000002_privacy`), channel-gateway, campaigns, analytics, the chat widget and the studio.

## Context

The platform talks to members of the public on behalf of several areas. Laws that may apply (Ley 1581 in Colombia, GDPR, ePrivacy, CAN-SPAM, the EU AI Act) require prior consent for optional processing, proof of that consent, special care for children's data, a working unsubscribe, and the ability to delete one person's data on request. ADR 0003 already keeps areas apart; these rules must follow the same isolation.

## Decision

### Collect less

- A chat user is a random 128-bit conversation ID (`usr-` + 32 hex characters). The gateway refuses IDs that do not look random, so names and emails cannot be used as IDs. There is no shared default ID.
- `InboundMessage` no longer has `location` or `media` (fields 8 and 9 are reserved). `context` carries only allow-listed campaign tags, and only when the person opted into campaign attribution.
- Errors from the engine are never logged or returned with the person's text.
- The widget's page event says that a message was sent, not what it said, unless the site opts in.
- Analytics accepts only named events with allow-listed, code-like properties, refuses unknown fields, and keeps totals. It has no per-person record, so it needs no erasure path.

### Consent

- Strictly necessary storage (sign-in, the conversation in the tab, the record of a choice) runs by default and is listed in `docs/legal/COOKIES_AND_STORAGE.md`.
- Analytics and campaign attribution are off until the person turns them on. Declining takes the same number of clicks as accepting and uses the same style.
- Where a bot needs explicit authorization or an age limit, the widget shows a pre-chat step with unchecked boxes and links to the policies. The chat does not start until the proof is stored: `POST /channels/webchat/consent` → studio-api `subject_consents`, with the policy version and the purposes, but no IP address or browser data.
- A bot's `audience` is `general` or `minors`. For minors, the widget asks for a parent's or guardian's permission (a checkbox, or the hosting page's own verified process through `grantParentalConsent(reference)`), and publishing requires the owner to confirm a parental-consent process exists. The confirmation is in the audit log and the `bot.published` event carries the audience.

### Erasure

- `Store.EraseSubject(workspace, subject)` runs in one transaction under the workspace's row-level security. It deletes what PostgreSQL holds (consent proof; campaign delivery records), adds a hashed suppression for email subjects so the address is never mailed again, writes a `data_subject.erasure_requested` outbox event for the stores outside studio-api (sessions, analytics, vectors), and writes a ledger row. The ledger and the audit log keep only a SHA-256 of the identifier, scoped by workspace (`libs/go/platform/privacy`).
- Three entry points: the chat widget (holding the conversation ID is the verification; the gateway calls `/internal/workspaces/{id}/data-deletion-requests` with `INTERNAL_API_TOKEN` and its own binding's workspace), workspace owners in the studio (with a required note on how identity was checked), and staff requesting their own deletion (executed by a platform admin, never removing an area's last owner).
- Every table must be classified in `tableDataClasses`; a test fails when a migration adds an unclassified table.

### Campaigns

- Every email gets `List-Unsubscribe` (HTTPS, plus optional `mailto:`) and `List-Unsubscribe-Post: List-Unsubscribe=One-Click`, a visible unsubscribe link in each body, and the sender's legal name and postal address. The service refuses to start without them.
- Unsubscribe tokens are HMAC-signed `(workspace, email hash, campaign)`; the address never appears in a URL. GET (the body link) and POST (RFC 8058) both unsubscribe at once, with no sign-in or confirmation step.
- The suppression list is checked for each recipient right before sending. If it cannot be read, the recipient is not emailed. The application role can add suppressions but not delete them.

## Consequences

- Each service that starts storing data about people (engine sessions, analytics events, knowledge vectors) must consume the erasure event before it goes live.
- `INTERNAL_API_TOKEN` is a shared secret between the gateway and studio-api; replace it with workload identity when the services move to the cloud.
- A hash of an email or chat ID is a pseudonym, not anonymous data: it is still personal data and stays under the same protections.
