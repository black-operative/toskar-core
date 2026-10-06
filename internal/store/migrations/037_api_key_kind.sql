-- A key's kind (#216): empty for a key made in API Access, which reaches the
-- whole control API, or "device" for one a phone got by pairing, which
-- reaches only what the phone uses.
ALTER TABLE api_keys ADD COLUMN kind TEXT NOT NULL DEFAULT '';
