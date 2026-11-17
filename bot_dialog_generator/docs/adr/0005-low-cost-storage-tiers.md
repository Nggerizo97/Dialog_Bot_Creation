# ADR 0005: Low-cost storage tiers

## Status

Proposed. Extends ADR 0002.

## Context

Reads and writes must scale with traffic while staying cheap, including when most workspaces are idle. ADR 0002 assigned data to stores by access pattern. This ADR adds the cost dimension and the stores for the knowledge layer (ADR 0004).

## Decision

Each kind of data goes to the cheapest store that fits how it is read and written. No component runs as an always-on cluster that sits idle.

| Data | Access pattern | Local (Docker + AWS Emulator) | Production choice | Why it is cheap |
|---|---|---|---|---|
| Live sessions, idempotency keys, rate limits, semantic cache | One read and one write per message; short-lived | Valkey 8 container | ElastiCache Serverless for Valkey | Billed by usage; TTLs delete data automatically |
| Authoring data, memberships, audit log, outbox | Relational, low volume, needs RLS | Postgres 17 container | Aurora PostgreSQL Serverless v2. Dev and QA auto-pause to 0 ACU | No compute charge while paused, only storage |
| Knowledge vectors (first stage) | Similarity search, filtered by workspace | Postgres + pgvector (HNSW) | Same Aurora cluster with pgvector | No extra service; RLS covers vectors too |
| Knowledge vectors (at scale) | Large corpora, mostly cold | S3 Vectors on AWS Emulator | Amazon S3 Vectors, one index per workspace | Object-storage pricing; up to 2 billion vectors per index and 10,000 indexes per bucket |
| Source documents, signed bot definitions, media | Written rarely, read on demand | S3 on AWS Emulator | S3 with lifecycle rules for old versions | Cheapest durable storage |
| Conversation events, AI usage, analytics | Append-only; analytical reads | S3 + DuckDB | DuckLake (Parquet on S3) queried with DuckDB on demand | No warehouse to keep running |

### Rules

- **Dev and QA scale to zero.** Aurora auto-pauses. Production keeps a small minimum ACU, because resuming from pause takes up to about 15 seconds.
- **Move vectors to S3 Vectors when it pays.** Revisit at about 10 million chunks, or when vector storage dominates the Postgres bill. S3 Vectors answers frequent queries in about 100 ms and infrequent ones in under a second. That is acceptable for a chatbot, where the model call takes longer than the search. The `VectorIndex` port makes the switch a configuration change per workspace.
- **Don't call the model when retrieval finds nothing** (ADR 0004). This is the largest single saving in the AI path.
- **Keep the cached prompt prefix stable** (ADR 0004). Cached input tokens cost a fraction of regular ones.
- **Scale workers on queue lag**, not on CPU or a fixed replica count.
- **Every store is partitioned or keyed by `workspace_id`.** Cost per area can then be measured and charged back.

### Local development

AWS Emulator (`aws-emulator` on `https://127.0.0.1:9297`, built from `infra/local/aws-emulator/Dockerfile`) provides local AWS API emulation for S3, S3 Vectors, KMS, SQS, Secrets Manager, STS, and IAM with SigV4 authentication (`~/.aws/emulator/9297/`) and telemetry disabled (`EMULATOR_TELEMETRY=off`). PostgreSQL runs alongside it in `infra/local/compose.yml`. When pgvector is needed in milestone 5, switch the Postgres image to `pgvector/pgvector:pg17` in `infra/local/compose.yml`.

## Consequences

- Two vector-store adapters must exist eventually. Only pgvector is built for the MVP.
- Aurora's resume delay rules out auto-pause in production.
- Cost per workspace becomes a first-class metric in analytics.
