-- Bloque P6 — stateful streaming transforms.
--
-- Foundry's "Streaming stateful transforms" + "Streaming keys" docs
-- describe per-key state with a TTL — `key_by` then `state_ttl`.
-- Stored on the window so the runtime can decide how to partition
-- and how long to retain operator state.

ALTER TABLE streaming_windows
    ADD COLUMN IF NOT EXISTS keyed BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS key_columns JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS state_ttl_seconds INTEGER NOT NULL DEFAULT 0
        CHECK (state_ttl_seconds >= 0 AND state_ttl_seconds <= 31536000);

COMMENT ON COLUMN streaming_windows.keyed IS
    'Foundry-parity: when true, the operator runs key_by(key_columns) before windowing.';
COMMENT ON COLUMN streaming_windows.key_columns IS
    'JSON array of stream field names. Empty when keyed=false.';
COMMENT ON COLUMN streaming_windows.state_ttl_seconds IS
    'Operator state retention (seconds). 0 disables TTL (state is kept until checkpoint reset).';
