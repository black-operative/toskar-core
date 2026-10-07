-- What a run's tools read, fingerprinted, so "notify on change" can tell a
-- run that read the same pages as the last one hasn't changed (#204).
ALTER TABLE automation_runs ADD COLUMN source_hash TEXT;
