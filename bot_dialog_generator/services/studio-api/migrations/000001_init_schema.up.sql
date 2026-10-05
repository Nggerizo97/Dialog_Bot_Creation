-- 000001_init_schema.up.sql: Authoring, draft versioning, nodes, active pointers, and transactional outbox

CREATE TABLE IF NOT EXISTS bots (
    id VARCHAR(64) PRIMARY KEY,
    tenant VARCHAR(64) NOT NULL DEFAULT 'default',
    name VARCHAR(255) NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS bot_versions (
    id VARCHAR(64) PRIMARY KEY,
    bot_id VARCHAR(64) NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    version VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'draft', -- draft, published, archived
    entry_node_id VARCHAR(64),
    artifact_uri TEXT,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_bot_version UNIQUE (bot_id, version)
);

CREATE TABLE IF NOT EXISTS dialogs (
    id VARCHAR(64) PRIMARY KEY,
    version_id VARCHAR(64) NOT NULL REFERENCES bot_versions(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    entry_node_id VARCHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS nodes (
    id VARCHAR(64) PRIMARY KEY,
    version_id VARCHAR(64) NOT NULL REFERENCES bot_versions(id) ON DELETE CASCADE,
    dialog_id VARCHAR(64) REFERENCES dialogs(id) ON DELETE CASCADE,
    type VARCHAR(32) NOT NULL, -- Trigger, Response, Menu, Service, Jump
    title VARCHAR(255) NOT NULL,
    detail TEXT,
    position_x REAL NOT NULL DEFAULT 0,
    position_y REAL NOT NULL DEFAULT 0,
    properties JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS transitions (
    id VARCHAR(64) PRIMARY KEY,
    version_id VARCHAR(64) NOT NULL REFERENCES bot_versions(id) ON DELETE CASCADE,
    dialog_id VARCHAR(64) REFERENCES dialogs(id) ON DELETE CASCADE,
    from_node_id VARCHAR(64) NOT NULL,
    to_node_id VARCHAR(64) NOT NULL,
    condition TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS active_version_pointers (
    bot_id VARCHAR(64) PRIMARY KEY REFERENCES bots(id) ON DELETE CASCADE,
    active_version VARCHAR(32) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS outbox_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type VARCHAR(64) NOT NULL,
    aggregate_id VARCHAR(64) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending', -- pending, published, failed
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_bot_versions_bot ON bot_versions(bot_id);
CREATE INDEX IF NOT EXISTS idx_nodes_version ON nodes(version_id);
CREATE INDEX IF NOT EXISTS idx_transitions_version ON transitions(version_id);
CREATE INDEX IF NOT EXISTS idx_outbox_pending ON outbox_events(status) WHERE status = 'pending';
