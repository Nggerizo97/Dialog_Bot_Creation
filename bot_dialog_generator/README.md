# Bot_Dialog_Generator

Bot_Dialog_Generator is a platform for building and running conversational bots for every area of an organization.

## Product direction

This repository is the MVP for a single bot platform shared by every area of the organization. Each area gets a private workspace with its own bots, flows and AI knowledge base, and only platform administrators can see across workspaces. Read [`docs/VISION.md`](docs/VISION.md) before starting new work.

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