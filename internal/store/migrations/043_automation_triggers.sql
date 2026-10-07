-- A trigger runs an automation when a page or feed changes, checked on its
-- schedule (#204): what to watch, what the last check found, and when it
-- was.
ALTER TABLE automations ADD COLUMN trigger_json TEXT;
ALTER TABLE automations ADD COLUMN watch_state TEXT;
ALTER TABLE automations ADD COLUMN last_checked_at TEXT;
