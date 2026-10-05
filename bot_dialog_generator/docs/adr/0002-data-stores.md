# ADR 0002: Operational data stores

## Status

Accepted

## Decision

- Valkey holds live conversation sessions, idempotency keys, rate-limit buckets, and short-lived media caches.
- Postgres holds Studio authoring data, publishing pointers, contacts, approvals, and the transactional outbox.
- S3 holds signed compiled definitions, media, and DuckLake Parquet data.
- DuckDB and DuckLake serve analytics, debugger replay, campaign audience ingest, and campaign reporting.

## Consequences

DuckDB is not used for conversation session state: concurrent per-message read-modify-write traffic requires a key-value store. Analytics writers are append-oriented and decoupled from the conversation path.