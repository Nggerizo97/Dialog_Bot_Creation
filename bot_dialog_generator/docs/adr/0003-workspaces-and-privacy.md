# ADR 0003: Workspaces and privacy isolation

## Status

Proposed

## Context

Each area of the organization must be able to build its own bots and flows on the shared platform without seeing other areas' work. Only platform administrators may see every bot. Today the studio API takes the tenant from the client-controlled `X-Tenant-ID` header, and routes such as `GET /bots/{id}`, `/versions` and `/publish` do not check ownership. Anyone who knows or guesses a bot ID can read or publish it.

## Decision

### Hierarchy and naming

`Organization → Workspace (one per area) → Bot → Version → Flow → Node`.

- The Protobuf `tenant` field keeps its meaning of **organization**.
- A new `workspace_id` field is added to `BotDefinition`, `InboundMessage`, `OutboundBatch` and the engine `Session`.
- The Kafka partition key from ADR 0001 becomes `workspace_id:channel:user_id`.
- All IDs are random (UUIDv7), never sequential or derived from names, so they cannot be guessed.

### Identity

- Users authenticate with OIDC: the company's identity provider (for example Entra ID) and Cognito on Floci for local work. The API trusts only claims from a verified token.
- `workspace_members(workspace_id, subject, role)` and `workspace_groups(workspace_id, idp_group, role)` map users and identity-provider groups to workspaces. Roles are `owner`, `editor` and `analyst`. `platform_admin` is a separate grant.
- A client may send a header to *select* among the workspaces the user belongs to. The server rejects any selection that is not in the user's memberships. The header is never a source of authority.

### Enforcement in depth

Every layer enforces isolation independently, so one bug does not leak data.

| Layer | Rule |
|---|---|
| API | Each request resolves a `Principal{subject, memberships, is_platform_admin}`. Repository methods take the workspace scope as a required argument. Look-ups are by `(workspace_id, id)`. A resource in another workspace returns **404, not 403**, so its existence is not revealed. |
| Database | Postgres row-level security on every workspace-owned table. Policy: `workspace_id = ANY(current_setting('app.workspace_ids')::uuid[])`. The service connects as a role that does not own the tables, and sets the scope per transaction with `SET LOCAL`. Platform admin reads use a separate policy and write an audit row. |
| Object storage | Keys are `workspaces/{workspace_id}/…` for definitions, documents and media. Production uses a per-workspace KMS data key (envelope encryption), so deleting a workspace can crypto-shred its data. |
| Runtime | Sessions use the key `sess:{workspace}:{channel}:{user}`. A channel binding (WhatsApp number, Teams app, widget key) belongs to exactly one bot in one workspace. Inbound messages are routed by the binding, never by message content. |
| Knowledge | Every vector carries `workspace_id` and `kb_id`. The retrieval filter is applied by the server from the bot's workspace and is never taken from the prompt or the model. With pgvector, RLS covers it. With S3 Vectors, each workspace gets its own index. |
| Analytics | DuckLake tables are partitioned by `workspace_id`. The analytics API injects the filter. Admin cross-workspace queries are audited. |

### Visibility

- Workspace members see only their workspace's bots, flows, knowledge bases, analytics and member list. There is no organization-wide search for non-admins.
- Platform admins can list and inspect every workspace. Reading conversation transcripts requires a stated reason (break-glass) and is audited.
- Templates and components live in a `platform` workspace that every workspace can read and none can write (see VISION.md).

### Audit

`audit_log(at, subject, workspace_id, action, resource, reason)` records every platform-admin access, publish, rollback, membership change and break-glass read. It is append-only.

## Consequences

- Every new table needs a `workspace_id` column and an RLS policy. A migration check fails the build if either is missing.
- An **isolation test suite** is required. For every route, a principal from workspace B must get 404 on workspace A's resources. SQL-level tests confirm RLS blocks direct queries.
- The current MVP endpoints must be reworked before any real area data is loaded: authenticate, scope by principal, remove the `X-Tenant-ID` trust, replace the hardcoded `demo` artifact path and restrict CORS to known origins.
- ADR 0001's partition key and contracts change. The change is cheap now and expensive after channels go live.
