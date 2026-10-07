-- A webhook trigger's link (#204): only the SHA-256 of its token is kept,
-- so the link can be checked but never read back.
ALTER TABLE automations ADD COLUMN hook_hash TEXT;
CREATE UNIQUE INDEX automations_hook_hash ON automations(hook_hash) WHERE hook_hash IS NOT NULL;
