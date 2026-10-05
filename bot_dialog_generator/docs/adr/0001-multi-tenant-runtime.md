# ADR 0001: Multi-tenant event-driven runtime

## Status

Accepted. Amended by [ADR 0003](0003-workspaces-and-privacy.md): the Kafka key becomes `workspace_id:channel:user_id`, and contracts gain `workspace_id`.

## Decision

All bots run on one stateless conversation-engine consumer group. Channel gateways acknowledge verified inbound webhooks, deduplicate by message identifier, and publish them to `conv.inbound`. The Kafka key is `tenant:channel:user_id`, preserving ordering for a user while allowing horizontal scale.

Bot definitions are signed Protobuf artifacts in versioned object storage. A session pins its active definition version. Publishing updates the pointer for new sessions and emits `bot.published`; it never restarts engine pods.

## Consequences

The engine must be deterministic for `(definition, session, input)`. Session state cannot be held in memory or in DuckDB.