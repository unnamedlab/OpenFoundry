-- Bloque E3 — RBAC + markings.
--
-- Adds `default_marking` to streams so that read paths can filter
-- against the caller's clearance set (`auth_middleware::Claims::allows_marking`).
-- The column is `TEXT` (marking *name*, not id) to match how Foundry
-- claims store clearances; `NULL` means "public" and bypasses the check.

ALTER TABLE streaming_streams
    ADD COLUMN IF NOT EXISTS default_marking TEXT;

CREATE INDEX IF NOT EXISTS idx_streaming_streams_default_marking
    ON streaming_streams (default_marking)
    WHERE default_marking IS NOT NULL;
