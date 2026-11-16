-- 000002_privacy.down.sql
DROP TABLE IF EXISTS staff_erasure_requests;
DROP TABLE IF EXISTS campaign_sends;
DROP TABLE IF EXISTS campaign_suppressions;
DROP TABLE IF EXISTS data_subject_requests;
DROP TABLE IF EXISTS subject_consents;
ALTER TABLE bots DROP COLUMN IF EXISTS min_age, DROP COLUMN IF EXISTS audience;
