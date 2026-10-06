# Bot_Dialog_Generator

Bot_Dialog_Generator is a platform for building and running conversational bots for every area of an organization.

## Product direction

This repository is the MVP for a single bot platform shared by every area of the organization. Each area gets a private workspace with its own bots, flows and AI knowledge base, and only platform administrators can see across workspaces. Read [`docs/VISION.md`](docs/VISION.md) before starting new work, and pick the next task from [`docs/PLAN.md`](docs/PLAN.md).

| ADR | Decision |
|---|---|
| [0001](docs/adr/0001-multi-tenant-runtime.md) | One stateless, event-driven engine runs every bot |
| [0002](docs/adr/0002-data-stores.md) | Operational data stores by access pattern |
| [0003](docs/adr/0003-workspaces-and-privacy.md) | Workspaces per area, with privacy enforced at every layer |
| [0004](docs/adr/0004-organizational-knowledge-and-ai-nodes.md) | Organizational knowledge (RAG) and AI nodes |
| [0005](docs/adr/0005-low-cost-storage-tiers.md) | Low-cost storage tiers, including vectors |

## Included foundation

- Canonical Protobuf contracts in `contracts/proto`.
- Stateless Go entry points for studio, channel gateway, conversation engine, campaigns, and analytics.
- React studio and Lit web-chat source workspaces.
- Floci-based local topology and architecture decisions.

## Prerequisites

- Go 1.25 or later
- Node.js 20 or later
- Docker Desktop for the local Floci stack

## First checks

```powershell
go test ./...
npm --prefix apps/studio-web install
npm --prefix apps/studio-web run build
docker compose -f infra/local/compose.yml up
```

The services expose `/livez` and `/readyz`. They are intentionally dependency-free until the Kafka, Valkey, Postgres, and object-storage adapters are added in later milestones.

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
| `AUTH_MODE` | `dev` or `oidc` | `dev` locally, `oidc` elsewhere |
| `OIDC_ISSUER` | Identity provider issuer URL (Entra ID, Cognito, ...) | required in `oidc` mode |
| `OIDC_AUDIENCE` | Expected token audience | required in `oidc` mode |
| `OIDC_GROUPS_CLAIM` | Claim that lists the caller's groups | `groups` (`cognito:groups` for Cognito) |
| `PLATFORM_ADMIN_GROUP` | Group whose members are platform admins | `bdg-platform-admins` |
| `CORS_ALLOWED_ORIGINS` | Comma-separated browser origins | `http://localhost:5173,http://localhost:5174` |

The studio reads the API address from `VITE_STUDIO_API_URL` (default `http://localhost:8080`).