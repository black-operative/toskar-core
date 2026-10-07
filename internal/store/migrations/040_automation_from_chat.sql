-- An automation made from a chat (#204): the conversation it came from,
-- where its results can go, and the draft the chat showed, so pressing
-- Create twice makes one automation.
ALTER TABLE automations ADD COLUMN conversation_id TEXT;
ALTER TABLE automations ADD COLUMN draft_id TEXT;
CREATE UNIQUE INDEX automations_draft_id ON automations(draft_id) WHERE draft_id IS NOT NULL;
