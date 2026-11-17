# Bot_Dialog_Generator

Bot_Dialog_Generator is a platform for building and running conversational bots for every area of an organization.

## Product direction

This repository is the MVP for a single bot platform shared by every area of the organization. Each area gets a private workspace with its own bots, flows and AI knowledge base, and only platform administrators can see across workspaces. Read [`docs/VISION.md`](docs/VISION.md) before starting new work, and pick the next task from [`docs/PLAN.md`](docs/PLAN.md). The Privacy Policy, Terms of Use, Refund Policy, cookie and storage inventory, business contact, third-party and license inventories, and the compliance risks are in [`docs/legal/`](docs/legal/README.md) (draft templates for legal review). The studio shows them at `#/legal/<page>` and links them from every page's footer.

| ADR | Decision |
|---|---|
| [0001](docs/adr/0001-multi-tenant-runtime.md) | One stateless, event-driven engine runs every bot |
| [0002](docs/adr/0002-data-stores.md) | Operational data stores by access pattern |
| [0003](docs/adr/0003-workspaces-and-privacy.md) | Workspaces per area, with privacy enforced at every layer |
| [0004](docs/adr/0004-organizational-knowledge-and-ai-nodes.md) | Organizational knowledge (RAG) and AI nodes |
| [0005](docs/adr/0005-low-cost-storage-tiers.md) | Low-cost storage tiers, including vectors |
| [0006](docs/adr/0006-consent-and-data-subject-rights.md) | Consent, data minimization and the right to erasure |

## Included foundation

- Canonical Protobuf contracts in `contracts/proto`.
- Stateless Go entry points for studio, channel gateway, conversation engine, campaigns, and analytics.
- React studio and Lit web-chat source workspaces.
- Floci-based local topology and architecture decisions.

## Prerequisites

- Go 1.25 or later
- Node.js 20 or later
- Podman (with `podman compose`) or Docker, for the local PostgreSQL and Floci stack

## First checks

```powershell
go test ./...
npm --prefix apps/studio-web install
npm --prefix apps/studio-web run build
podman compose -f infra/local/compose.yml up -d
```

The services expose `/livez` and `/readyz`.

### Local database

`infra/local/compose.yml` runs PostgreSQL 17 on host port **5433** (5432 is left free for a PostgreSQL installed on the machine). Apply the schema as its owner, then run studio-api as the application role, which owns no tables, so row-level security always applies:

```powershell
$env:DATABASE_ADMIN_URL = "postgres://bdg_owner:bdg_owner_local@localhost:5433/bot_dialog_generator"
go run ./services/studio-api migrate
$env:DATABASE_URL = "postgres://studio_api:studio_api_local@localhost:5433/bot_dialog_generator"
$env:SEED_DEMO = "true"
go run ./services/studio-api
```

Without `DATABASE_URL`, studio-api uses the in-memory demo store and loses its data on restart.

To run the studio-api tests against PostgreSQL as well (each test gets its own throwaway database):

```powershell
$env:TEST_DATABASE_URL = "postgres://bdg_owner:bdg_owner_local@localhost:5433/postgres"
$env:STUDIO_TEST_STORE = "postgres"
go test ./services/studio-api/...
```

## Local sign-in

Every Studio API route except the health checks needs a bearer token, and each workspace (one per area) is private: non-members get 404 for anything inside it.

Run the API and the studio, then open http://localhost:5173 and pick a demo user:

```powershell
go run ./services/studio-api
npm --prefix apps/studio-web run dev
```

| User | Access |
|---|---|
| alice | Owner of Customer service |
| erin | Editor of Customer service |
| carol | Analyst of Customer service (read-only) |
| bob | Owner of Human resources |
| frank | Editor of Human resources, through the `hr-team` group |
| dana | Platform admin (`bdg-platform-admins` group): sees every workspace, every access audited |

Locally the API runs with `AUTH_MODE=dev`, where `POST /dev/token` mints a signed token for any user. It refuses to start in dev mode unless `APP_ENV` is unset, `local` or `dev`.

To call the API from a terminal:

```powershell
$token = (Invoke-RestMethod -Method Post http://localhost:8080/dev/token -Body '{"subject":"bob"}').access_token
Invoke-RestMethod http://localhost:8080/workspaces/ws-hr/bots -Headers @{ Authorization = "Bearer $token" }
```

### Configuration

| Variable | Meaning | Default |
|---|---|---|
| `APP_ENV` | `local`, `dev`, `staging`, `production`, ... | `local` |
| `AUTH_MODE` | `dev`, `oidc` or `cognito` | `dev` locally, `oidc` elsewhere |
| `OIDC_ISSUER` | Identity provider issuer URL (Entra ID, Cognito, ...) | required in `oidc` mode |
| `OIDC_AUDIENCE` | Expected token audience | required in `oidc` mode |
| `OIDC_GROUPS_CLAIM` | Claim that lists the caller's groups | `groups` (`cognito:groups` for Cognito) |
| `OIDC_SUBJECT_CLAIM` | Claim that identifies the caller | `sub` (`oid` for Entra ID) |
| `COGNITO_USER_POOL_ID`, `COGNITO_REGION`, `COGNITO_CLIENT_IDS` | Amazon Cognito user pool, its region and the studio's app client (`cognito` mode) | required in `cognito` mode |
| `COGNITO_ISSUER`, `OIDC_CA_FILE` | Issuer and certificate of a local AWS Emulator (`cognito` mode) | unset: the pool on AWS |
| `PLATFORM_ADMIN_GROUP` | Group whose members are platform admins | `bdg-platform-admins` |
| `CORS_ALLOWED_ORIGINS` | Comma-separated browser origins | `http://localhost:5173,http://localhost:5174` |
| `DATABASE_URL` | PostgreSQL as the application role | unset: in-memory store |
| `SEED_DEMO` | `true` loads the demo organization into an empty database (local and dev only) | off |
| `DATABASE_ADMIN_URL` | Schema owner, used only by `studio-api migrate` | required for `migrate` |
| `INTERNAL_API_TOKEN` | Shared secret (32+ characters) for the channel gateway's `/internal` routes: consent proof and "Delete my data" from the chat widget | unset: routes not mounted |

### Sign in with Amazon Cognito

Cognito user pools work in place of Entra ID, locally through the [AWS Emulator](https://github.com/aws/aws-emulator) and in production with a real pool. Step by step: [`docs/cognito-setup.md`](docs/cognito-setup.md). Locally, in short:

```powershell
aws-emulator --telemetry off                # or: podman compose -f infra/local/compose.yml --profile cognito up --build aws-emulator
go run ./tools/cognito-local -write-env     # pool, groups and demo users; prints studio-api's settings
go run ./services/studio-api                # with the printed settings (AUTH_MODE=cognito)
npm --prefix apps/studio-web run dev:cognito
```

The demo users are the same six as above, with the password the tool prints.

The studio reads the API address from `VITE_STUDIO_API_URL` (default `http://localhost:8080`). Setting `VITE_OIDC_AUTHORITY` and `VITE_OIDC_CLIENT_ID` (see `apps/studio-web/.env.example`) replaces the demo users with **Sign in with Microsoft**. Step-by-step Entra ID setup: [`docs/entra-setup.md`](docs/entra-setup.md).

The channel gateway reads `WEBCHAT_WORKSPACE_ID`, the workspace its web chat serves (default `ws-customer-service`). It is set on the server, never taken from the chat request, so a visitor cannot reach another area's bot.

### Privacy, consent and campaigns

The chat widget asks for consent and offers **Delete my data**; the channel gateway hands both to studio-api. Set the same `INTERNAL_API_TOKEN` on both services:

| Service | Variable | Meaning | Default |
|---|---|---|---|
| channel-gateway | `STUDIO_API_URL` | studio-api address for consent proof and erasure requests | unset: kept in memory (local only) |
| channel-gateway | `INTERNAL_API_TOKEN` | Same value as studio-api's | required with `STUDIO_API_URL` |
| campaigns | `DATABASE_URL` | PostgreSQL as an application role; the do-not-contact list lives here | required outside local and dev |
| campaigns | `UNSUBSCRIBE_SIGNING_KEY` | Secret (32+ characters) that signs unsubscribe links | required outside local and dev |
| campaigns | `PUBLIC_BASE_URL` | Public HTTPS address used in unsubscribe links | `http://localhost:8083` locally |
| campaigns | `SENDER_LEGAL_NAME`, `SENDER_POSTAL_ADDRESS`, `SENDER_FROM` | Printed in every email; the service refuses to start without them outside local | demo values locally |
| campaigns | `SMTP_ADDR`, `SMTP_USERNAME`, `SMTP_PASSWORD` | Relay; must DKIM-sign for the From domain | unset: mail is logged, not sent (local only) |
| studio-web | `VITE_ANALYTICS_URL` | Analytics service; turns on the privacy banner. Nothing is sent before opt-in | unset: no banner, no statistics |
| studio-web | `VITE_LEGAL_*` | Business details for the footer (see `apps/studio-web/.env.example`) | placeholders shown |

The widget's privacy attributes (`privacy-url`, `terms-url`, `cookies-url`, `policy-version`, `require-authorization`, `min-age`, `audience`, `parental-consent`, `analytics-url`, `campaign-attribution`) are described in [docs/legal/README.md](docs/legal/README.md) and in `apps/webchat-widget/index.html`.

