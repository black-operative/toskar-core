-- Why a run did or didn't notify, as the key the apps show
-- (automations:notice.<detail>) with its values as JSON, so they explain
-- the server's decision instead of deciding again (#204).
ALTER TABLE automation_runs ADD COLUMN notify_detail TEXT;
ALTER TABLE automation_runs ADD COLUMN notify_values TEXT;
