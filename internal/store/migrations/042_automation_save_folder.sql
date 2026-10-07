-- A folder each result is also saved to as a Markdown file, and where a
-- run's result was saved (#204).
ALTER TABLE automations ADD COLUMN save_folder TEXT;
ALTER TABLE automation_runs ADD COLUMN saved_file TEXT;
