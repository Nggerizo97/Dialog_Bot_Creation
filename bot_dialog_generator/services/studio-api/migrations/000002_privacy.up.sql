-- 000002_privacy.up.sql: consent proof, data-subject requests and campaign records.
--
-- Everything here is about a person (a chat user, a campaign recipient or a member of
-- staff), so every table is listed in tableDataClasses (privacy_test.go) and is
-- covered by the erasure in EraseSubject. Tables that belong to one area are under
-- the same forced row-level security as migration 000001.

-- Who a bot is for. 'minors' means the bot targets or collects data from children:
-- the chat widget then needs an age gate and a parental-consent step, and publishing
-- needs the owner's confirmation that a parental-consent process exists.
ALTER TABLE bots
    ADD COLUMN audience TEXT NOT NULL DEFAULT 'general' CHECK (audience IN ('general', 'minors')),
    ADD COLUMN min_age  INTEGER NOT NULL DEFAULT 0 CHECK (min_age BETWEEN 0 AND 21);

-- Proof that a chat user accepted the notice (and which optional purposes they opted
-- into). Only what is needed as proof: no IP address, no browser details, no name.
CREATE TABLE subject_consents (
    id                 BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id       TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    channel            TEXT NOT NULL,
    subject_id         TEXT NOT NULL,
    purposes           JSONB NOT NULL DEFAULT '{}',
    policy_version     TEXT NOT NULL DEFAULT '',
    age_confirmed      BOOLEAN NOT NULL DEFAULT false,
    guardian_consent   BOOLEAN NOT NULL DEFAULT false,
    guardian_reference TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX subject_consents_subject ON subject_consents (workspace_id, channel, subject_id);

-- Ledger of erasure requests. It keeps a one-way hash of the subject, never the
-- conversation ID or email address itself.
CREATE TABLE data_subject_requests (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    subject_kind TEXT NOT NULL CHECK (subject_kind IN ('chat_user', 'email')),
    subject_hash TEXT NOT NULL,
    source       TEXT NOT NULL CHECK (source IN ('webchat', 'studio')),
    verification TEXT NOT NULL,
    requested_by TEXT NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('completed')),
    result       JSONB NOT NULL DEFAULT '[]',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX data_subject_requests_workspace ON data_subject_requests (workspace_id, created_at);

-- Do-not-contact list for campaigns. One-way hash only, so honoring an unsubscribe
-- never requires keeping the address.
CREATE TABLE campaign_suppressions (
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    email_hash   TEXT NOT NULL,
    reason       TEXT NOT NULL CHECK (reason IN ('unsubscribe', 'erasure', 'bounce', 'complaint')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, email_hash)
);

-- What was sent to whom, by hash. Written by the campaigns service for every recipient
-- it considered, including the ones it skipped.
CREATE TABLE campaign_sends (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    campaign_id  TEXT NOT NULL,
    email_hash   TEXT NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('sent', 'skipped_suppressed', 'skipped_no_consent', 'failed')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX campaign_sends_recipient ON campaign_sends (workspace_id, email_hash);

-- A member of staff asking for their own data to be deleted. It spans every area, like
-- memberships, so it is handled by platform administrators and is not under
-- row-level security.
CREATE TABLE staff_erasure_requests (
    id          TEXT PRIMARY KEY,
    subject     TEXT NOT NULL,
    note        TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL CHECK (status IN ('pending', 'completed', 'partially_completed')),
    result      JSONB NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    resolved_by TEXT NOT NULL DEFAULT ''
);
CREATE INDEX staff_erasure_requests_subject ON staff_erasure_requests (subject);

ALTER TABLE subject_consents ENABLE ROW LEVEL SECURITY;
ALTER TABLE subject_consents FORCE ROW LEVEL SECURITY;
ALTER TABLE data_subject_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE data_subject_requests FORCE ROW LEVEL SECURITY;
ALTER TABLE campaign_suppressions ENABLE ROW LEVEL SECURITY;
ALTER TABLE campaign_suppressions FORCE ROW LEVEL SECURITY;
ALTER TABLE campaign_sends ENABLE ROW LEVEL SECURITY;
ALTER TABLE campaign_sends FORCE ROW LEVEL SECURITY;

CREATE POLICY workspace_isolation ON subject_consents
    USING (workspace_id = current_setting('app.workspace_id', true)
           OR current_setting('app.all_workspaces', true) = 'on')
    WITH CHECK (workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY workspace_isolation ON data_subject_requests
    USING (workspace_id = current_setting('app.workspace_id', true)
           OR current_setting('app.all_workspaces', true) = 'on')
    WITH CHECK (workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY workspace_isolation ON campaign_suppressions
    USING (workspace_id = current_setting('app.workspace_id', true)
           OR current_setting('app.all_workspaces', true) = 'on')
    WITH CHECK (workspace_id = current_setting('app.workspace_id', true));
CREATE POLICY workspace_isolation ON campaign_sends
    USING (workspace_id = current_setting('app.workspace_id', true)
           OR current_setting('app.all_workspaces', true) = 'on')
    WITH CHECK (workspace_id = current_setting('app.workspace_id', true));

GRANT SELECT, INSERT, DELETE ON subject_consents, campaign_sends TO bdg_app;
GRANT SELECT, INSERT ON data_subject_requests TO bdg_app;
-- A suppression is only ever added: the application cannot lift an opt-out.
GRANT SELECT, INSERT ON campaign_suppressions TO bdg_app;
GRANT SELECT, INSERT, UPDATE ON staff_erasure_requests TO bdg_app;
