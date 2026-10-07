-- 000001_init_schema.down.sql
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS bot_versions;
DROP TABLE IF EXISTS bots;
DROP TABLE IF EXISTS workspace_group_grants;
DROP TABLE IF EXISTS workspace_members;
DROP TABLE IF EXISTS workspaces;
