-- 000001_init_schema.up.sql: studio-api storage, with each area's content isolated by
-- row-level security (ADR 0003).
--
-- studio-api connects as a login role that is a member of bdg_app and owns no
-- tables, so row-level security always applies to it. Every workspace-scoped
-- transaction sets app.workspace_id; platform-admin reads across workspaces set
-- app.all_workspaces = 'on'. Writes are always limited to app.workspace_id.

DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'bdg_app') THEN
        CREATE ROLE bdg_app NOLOGIN;
    END IF;
END
$$;

CREATE TABLE workspaces (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ
);

-- Memberships and group grants decide which workspaces a caller can see, so they are
-- read across workspaces and are not under row-level security.
CREATE TABLE workspace_members (
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    subject      TEXT NOT NULL,
    role         TEXT NOT NULL CHECK (role IN ('owner', 'editor', 'analyst')),
    PRIMARY KEY (workspace_id, subject)
);
CREATE INDEX workspace_members_subject ON workspace_members (subject);

CREATE TABLE workspace_group_grants (
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    group_id     TEXT NOT NULL,
    display_name TEXT NOT NULL,
    role         TEXT NOT NULL CHECK (role IN ('owner', 'editor', 'analyst')),
    PRIMARY KEY (workspace_id, group_id)
);
CREATE INDEX workspace_group_grants_group ON workspace_group_grants (group_id);

CREATE TABLE bots (
    id             TEXT PRIMARY KEY,
    workspace_id   TEXT NOT NULL REFERENCES workspaces (id),
    tenant         TEXT NOT NULL,
    name           TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    draft_version  TEXT NOT NULL,
    active_version TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, workspace_id)
);
CREATE INDEX bots_workspace ON bots (workspace_id);

-- A version's workspace must be its bot's workspace (composite foreign key).
CREATE TABLE bot_versions (
    workspace_id  TEXT NOT NULL,
    bot_id        TEXT NOT NULL,
    version       TEXT NOT NULL,
    status        TEXT NOT NULL CHECK (status IN ('draft', 'published', 'archived')),
    entry_node_id TEXT NOT NULL DEFAULT '',
    nodes         JSONB NOT NULL DEFAULT '[]',
    edges         JSONB NOT NULL DEFAULT '[]',
    artifact_uri  TEXT NOT NULL DEFAULT '',
    published_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (bot_id, version),
    FOREIGN KEY (bot_id, workspace_id) REFERENCES bots (id, workspace_id) ON DELETE CASCADE
);

-- Transactional outbox: written in the publishing transaction, relayed later.
CREATE TABLE outbox_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id TEXT NOT NULL REFERENCES workspaces (id),
    event_type   TEXT NOT NULL,
    payload      JSONB NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'published', 'failed')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ
);
CREATE INDEX outbox_events_pending ON outbox_events (created_at) WHERE status = 'pending';

-- Append-only: the application can add entries but never change or delete them.
CREATE TABLE audit_log (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    subject      TEXT NOT NULL,
    workspace_id TEXT,
    action       TEXT NOT NULL,
    resource     TEXT NOT NULL
);

-- Row-level security on each area's content. FORCE applies it to the table owner too.
ALTER TABLE bots ENABLE ROW LEVEL SECURITY;
ALTER TABLE bots FORCE ROW LEVEL SECURITY;
ALTER TABLE bot_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE bot_versions FORCE ROW LEVEL SECURITY;
ALTER TABLE outbox_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbox_events FORCE ROW LEVEL SECURITY;

CREATE POLICY workspace_isolation ON bots
    USING (workspace_id = current_setting('app.workspace_id', true)
           OR current_setting('app.all_workspaces', true) = 'on')
    WITH CHECK (workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY workspace_isolation ON bot_versions
    USING (workspace_id = current_setting('app.workspace_id', true)
           OR current_setting('app.all_workspaces', true) = 'on')
    WITH CHECK (workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY workspace_isolation ON outbox_events
    USING (workspace_id = current_setting('app.workspace_id', true)
           OR current_setting('app.all_workspaces', true) = 'on')
    WITH CHECK (workspace_id = current_setting('app.workspace_id', true));

GRANT SELECT, INSERT, UPDATE, DELETE
    ON workspaces, workspace_members, workspace_group_grants, bots, bot_versions, outbox_events
    TO bdg_app;
GRANT SELECT, INSERT ON audit_log TO bdg_app;
