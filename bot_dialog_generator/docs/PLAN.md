# Bot_Dialog_Generator: next-steps plan

This plan turns the [vision](VISION.md) into an ordered list of work. VISION.md says where the product is going; this file says what to build next, in what order, and how to know each step is done. Update it when a milestone closes.

Last updated: 5 Oct 2026, after the M1 area and member management slice.

## Where we are

| Area | State |
|---|---|
| Privacy between areas (studio-api) | **Done.** Signed tokens, workspaces under `/workspaces/{id}`, 404 for non-members, roles, audited admin view, isolation suite over every route |
| Studio web | Sign-in (dev only), workspace and bot pickers, role-aware designer, admin page |
| Storage | In memory. Everything is lost when studio-api restarts |
| Flow editor | Nodes can be added, edited and deleted. **Connections between nodes cannot be edited**, and the canvas places at most 6 nodes in fixed spots |
| Runtime | **Not connected.** The gateway answers with a hardcoded fake. The conversation-engine service is only a health check, and the studio's test chat is simulated. A published bot cannot be talked to yet |
| Contracts | `workspace_id` on bot definitions, inbound and outbound messages and engine sessions; the engine refuses cross-workspace runs |
| AI knowledge | Not started |
| Campaigns, analytics | Empty services |

The biggest gap is no longer privacy. It is that an area can design and publish a bot, but nobody can talk to it. The plan closes that gap before adding AI, because AI nodes run inside the conversation engine.

## Order of work

| # | Milestone | Why now | Size* |
|---|---|---|---|
| 0 | Housekeeping and decisions | Unblocks everything below | 2–3 days |
| 1 | Finish M1: areas and members | Areas can't be created or staffed today, only seeded | 1–1.5 weeks |
| 2 | Real storage | Nothing survives a restart; RLS is part of the privacy promise | 2–3 weeks |
| 3 | First real conversation | Design → publish → chat must work end to end | 2–3 weeks |
| 4 | Flow editor | Areas can't build real flows without drawing connections | 2 weeks |
| 5 | Knowledge base v1 (AI) | The main new value; needs milestones 2 and 3 | 3–4 weeks |
| 6 | AI Router, guardrails and cost limits | Makes AI safe and affordable per area | 2 weeks |
| 7 | Templates and components | Reuse across areas | 2–3 weeks |
| 8 | Channels and analytics | WhatsApp, Teams, dashboards per area | 3–4 weeks |
| 9 | Scale | Kafka, S3 Vectors, load tests | 3 weeks |

\*Rough sizes for one developer working with Claude. Re-estimate at the start of each milestone.

---

## 0. Housekeeping and decisions (2–3 days)

**Tooling**
- [ ] Sign in the GitHub CLI (`gh auth login`) so PRs can be opened from the terminal.
- [ ] Protect `main`: require the CI checks and a PR to merge.
- [ ] Install Docker Desktop. Floci (local AWS: Postgres, Valkey, S3, Cognito) needs it from milestone 2 on.
- [ ] Install `buf` for contract changes (`npm install -g @bufbuild/buf`).

**CI**
- [x] Add `gofmt -l` and `go vet` to CI. Fix the six files that are not gofmt-clean today (missing final newline / CRLF).
- [x] Add a Playwright job that runs `tests/e2e` (it now starts studio-api by itself).

**Decisions** (recorded in [VISION.md](VISION.md#decisions), 5 Oct 2026)
- [x] One organization for now; no multi-company features until a customer exists.
- [x] Identity provider: Microsoft Entra ID. Areas are assigned to Entra groups, so each person sees only their own areas.
- [x] Admins cannot read conversations; break-glass only to investigate a reported production issue.
- [x] AI answers in the user's language and only on the bot's topic.
- [x] No contact center yet; the transfer stays a placeholder.
- [ ] Model provider and region for AI (default proposal: Claude on Amazon Bedrock), and what data may be sent to it. Needed before milestone 5 uses a real model.

Done when: tools installed, CI checks formatting and runs e2e, and the decisions are written into VISION.md.

## 1. Finish M1: areas and members (1–1.5 weeks)

Admins can now create, rename, archive and restore areas, and owners manage people and Entra groups from the Members tab. Contracts now carry `workspace_id`. Still to do: Entra sign-in in the studio.

- [x] **Workspace management (admin):** `POST /admin/workspaces`, rename, archive and restore (`PATCH /admin/workspaces/{id}`). Audited. Archived areas are hidden from their members.
- [x] **Membership management (owners):** list, add and remove members and group grants in their own workspace. Owners cannot grant `platform_admin`. Every change is audited. Studio screen: "Members" tab.
- [x] **Entra groups as the main way in:** a group grant names an Entra group by its object ID (a GUID, which is what Entra puts in the `groups` claim) and shows its display name in the studio. Admins assign a group when creating an area.
- [x] **`workspace_id` in the contracts:** on `BotDefinition`, `InboundMessage`, `OutboundBatch` and the engine `Session`. The compiler requires it, the engine's `Step` refuses a definition, message or session from different workspaces (`ErrWorkspaceMismatch`), and the gateway takes it from its channel binding (`WEBCHAT_WORKSPACE_ID` until per-widget bindings in milestone 3), never from the request. The Kafka key `workspace_id:channel:user_id` applies when Kafka arrives (milestone 9).
- [ ] **Studio sign-in with Microsoft Entra ID:** OIDC Authorization Code + PKCE (MSAL.js or `oidc-client-ts`), tokens kept in memory, silent renew. App registration with `groupMembershipClaims` set to application groups only (avoids the 200-group overage), the platform-admin group assigned to the app, and `OIDC_ISSUER` / `OIDC_AUDIENCE` set from the registration. Develop against a free test Entra tenant; dev sign-in stays for local work only.

Done when: an admin creates "Legal", makes Ana its owner, Ana adds Luis as editor, and Luis builds a bot nobody outside Legal can see. Isolation suite covers the new routes automatically.

## 2. Real storage (2–3 weeks)

- [ ] **Postgres store** (pgx + sqlc) behind the existing `Store` interface. Migration `000002`: workspaces, members, group grants, audit log, `workspace_id` on every table.
- [ ] **Row-level security:** policies on every workspace table; the service connects as a non-owner role and sets `app.workspace_ids` per transaction. SQL-level tests prove a direct query cannot read another workspace's rows.
- [ ] **One test suite, two stores:** run the store and isolation tests against both the memory store and Postgres.
- [ ] **Bot definitions to S3** under `workspaces/{id}/bots/{bot}/{version}.pb`, signed with a KMS key (ADR 0001).
- [ ] **Transactional outbox relay:** publish `bot.published` from the outbox table.

Done when: restarting studio-api loses nothing, the isolation suite passes on Postgres, and the RLS tests pass. Requires Docker and Floci.

## 3. First real conversation (2–3 weeks)

- [ ] **Engine service:** load the active definition for a bot from S3 (cached, reloaded on `bot.published`), keep sessions in Valkey, pin each session to its definition version, run the existing `Step` function.
- [ ] **Channel bindings:** a web-chat widget key belongs to one bot in one workspace. The gateway resolves workspace and bot from the binding, never from the message.
- [ ] **Gateway → engine:** replace the hardcoded fake. Synchronous HTTP is fine at this stage; Kafka comes in milestone 9.
- [ ] **Studio test chat** talks to the real engine against the current draft.
- [ ] **CEL conditions** on edges instead of string comparison (no `eval`, compile-time errors in the studio).
- [ ] **Handoff choice in the seed bots:** "Connect to advisor" becomes a menu that offers the AI assistant or a contact-center agent, matching the demo script already in the gateway, widget and studio test chat.

Done when: Bob publishes HR helpdesk, opens the widget page and has the conversation he designed; republishing doesn't break an ongoing conversation.

## 4. Flow editor (2 weeks)

- [ ] Replace the fixed-position canvas with React Flow: drag nodes, draw and delete connections, set branch conditions, choose the entry node.
- [ ] Save positions with the draft.
- [ ] Show compile errors (missing trigger, edges to nowhere, bad conditions) next to the node before publishing.

Done when: someone who has never seen the code builds a 10-node flow with a menu and three branches, publishes it, and it works in the widget.

## 5. Knowledge base v1 (3–4 weeks)

Design in [ADR 0004](adr/0004-organizational-knowledge-and-ai-nodes.md) and [ADR 0005](adr/0005-low-cost-storage-tiers.md).

- [ ] **Knowledge bases per workspace:** create, upload PDF and DOCX to S3 under the workspace prefix, list and delete documents.
- [ ] **Ingestion worker:** extract text, chunk by heading, embed (multilingual model on Bedrock), store in pgvector with RLS.
- [ ] **Retrieval:** hybrid vector + full-text search, scoped by the bot's workspace on the server.
- [ ] **Knowledge node:** an AI effect from the engine; the AI service calls the model with retrieved chunks as cited documents; `answered`, `no_answer` and `handoff` branches. No model call when nothing relevant is found.
- [ ] **Any language:** the AI replies in the user's language, using a multilingual embedding model so a question in one language finds documents written in another.
- [ ] **On topic only:** the AI answers only questions about the bot's purpose and its knowledge base; unrelated questions (general knowledge, other areas, chit-chat beyond a greeting) get a polite refusal that says what the bot can help with.
- [ ] **Eval set per knowledge base:** golden questions with expected sources; runs on every document or prompt change.
- [ ] Locally, a fake model and embedding port (Floci's Bedrock returns dummy responses).

Done when: HR uploads its leave policy, the HR bot answers "how many vacation days do I have?" with a citation in English and "¿cuántos días de vacaciones tengo?" in Spanish, refuses "who won the World Cup?", and Customer service's bot cannot retrieve anything from HR's documents. The eval set includes off-topic and other-language questions.

## 6. AI Router, guardrails and cost limits (2 weeks)

- [ ] AI Router node: classify free text into the node's branches with a confidence threshold and an `unclear` branch.
- [ ] **AI first, person on request:** when a user asks for an advisor, the bot offers the AI assistant or a contact-center agent. The AI assistant always shows "Talk to a person", and escalates by itself on `no_answer`, low confidence, repeated failure or frustration. The agent receives the transcript and an AI summary so the user doesn't repeat themselves.
- [ ] PII redaction before text reaches the model, configurable per workspace.
- [ ] Allowed and blocked topics per workspace, enforced before and after the model call (on-topic rule from VISION.md).
- [ ] Token budgets and rate limits per workspace; usage events for cost per area.
- [ ] Prompt caching layout (platform rules → workspace persona → bot instructions).

Done when: each area sees its AI usage and cost, and a bot stops calling the model when its budget is spent.

## 7. Templates and components (2–3 weeks)

- [ ] Organization template library (admins publish; owners propose).
- [ ] Create a bot from a template; the fork remembers its source and version.
- [ ] Shared components (authentication, survey, handoff) called by version through a Jump node.
- [ ] Diff and merge when a template changes.

Done when: two areas start from the same template, and an improvement to the template reaches both without copy-paste.

## 8. Channels and analytics (3–4 weeks)

- [ ] WhatsApp Cloud API and Teams bindings per workspace, with webhook signature checks.
- [ ] Contact-center connector: hand the conversation to a live agent queue per workspace, with queue status and wait time shown to the user. *Deferred: no contact center yet; keep the transfer behind an interface with a placeholder.*
- [ ] **Break-glass transcript access:** admins request access to specific conversations with a reason and production-issue reference; access is time-limited and audited. No other admin path reads transcripts.
- [ ] Conversation events to DuckLake; per-workspace dashboards (volume, containment, AI answers, no-answer rate); admin roll-up.

## 9. Scale (3 weeks)

- [ ] Kafka between gateway and engine (key `workspace_id:channel:user_id`), KEDA scaling on lag.
- [ ] S3 Vectors adapter for large knowledge bases.
- [ ] Load test against the targets in ADR 0001.

---

## Known debt to pay along the way

| Item | Where | When |
|---|---|---|
| Six Go files not gofmt-clean | channel-gateway, analytics, campaigns, conversation-engine, httpserver | Milestone 0 |
| Dev session token in `sessionStorage` | studio-web | Replaced by OIDC in milestone 1 |
| `go test -race` can't run on Windows without cgo | local only (CI runs it) | Accept |
| Vite dev server fails on OneDrive with `EPERM` on `node_modules/.vite` | local only | Move the repo out of OneDrive, or set Vite `cacheDir` outside it |
| Test chat replies are simulated | studio-web | Milestone 3 |
| Campaigns and analytics services are empty | services | Milestones 8–9 |
| Legal templates need placeholders filled and legal review; Colombian authorization step and retention periods undecided | [docs/legal](legal/README.md) | Before the first public bot |
| No LICENSE file: decide proprietary or open source | repository root | Milestone 0 |

## Risks

- **Docker not available locally** blocks milestones 2, 3 and 5. Install it in milestone 0.
- **No model access** (AWS account, Bedrock permissions, data-classification approval) blocks milestone 5 in real use. Start the request in milestone 0; build against a fake model meanwhile.
- **Scope creep in the flow editor.** Keep milestone 4 to nodes, connections, conditions and errors; no styling work.
