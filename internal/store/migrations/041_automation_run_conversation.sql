-- The chat a run's result was posted to (#204), so "Continue in chat"
-- opens the same chat again instead of starting another.
ALTER TABLE automation_runs ADD COLUMN conversation_id TEXT;
