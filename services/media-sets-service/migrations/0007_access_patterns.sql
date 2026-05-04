-- H5 — access patterns + per-invocation usage ledger.
--
-- ## Tables
--
-- 1. `media_set_access_patterns` — one row per (media_set, kind)
--    registration. Carries the persistence policy + ttl_seconds the
--    runtime worker reads on every invocation. The natural key is
--    `(media_set_rid, kind)` because a set has at most one configured
--    instance of each kind; we keep `id` as the public RID and the
--    composite UNIQUE for the application-level dedup.
--
-- 2. `media_set_access_pattern_invocations` — append-only ledger of
--    every billed invocation. Backs `GET /media-sets/{rid}/usage` so
--    the Usage UI can chart compute-seconds + processed bytes per
--    `(kind, day)` without touching the Prometheus warehouse. Rows
--    are also the durable audit trail when the
--    `media_set.access_pattern_invoked` envelope downstream is
--    delayed by a Debezium / Kafka outage.
--
-- 3. `media_set_access_pattern_outputs` — for `PERSIST` and
--    `CACHE_TTL` policies, points at the derived artifact's storage
--    URI keyed by `(pattern_id, item_rid, params_hash)`. `expires_at`
--    is NULL for PERSIST (forever), set to `now() + ttl_seconds` for
--    `CACHE_TTL`. The application reads this row on a hit to
--    short-circuit the runtime call.

CREATE TABLE IF NOT EXISTS media_set_access_patterns (
    id              TEXT        PRIMARY KEY,
    media_set_rid   TEXT        NOT NULL REFERENCES media_sets(rid) ON DELETE CASCADE,
    kind            TEXT        NOT NULL,
    params          JSONB       NOT NULL DEFAULT '{}'::jsonb,
    persistence     TEXT        NOT NULL CHECK (persistence IN ('RECOMPUTE','PERSIST','CACHE_TTL')),
    -- Only consulted when persistence = 'CACHE_TTL'. Always >= 0.
    ttl_seconds     BIGINT      NOT NULL DEFAULT 0 CHECK (ttl_seconds >= 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      TEXT        NOT NULL DEFAULT '',
    UNIQUE (media_set_rid, kind)
);

CREATE INDEX IF NOT EXISTS idx_media_set_access_patterns_set
    ON media_set_access_patterns(media_set_rid);

CREATE TABLE IF NOT EXISTS media_set_access_pattern_invocations (
    id                 BIGSERIAL   PRIMARY KEY,
    media_set_rid      TEXT        NOT NULL REFERENCES media_sets(rid) ON DELETE CASCADE,
    pattern_id         TEXT        REFERENCES media_set_access_patterns(id) ON DELETE SET NULL,
    kind               TEXT        NOT NULL,
    item_rid           TEXT        NOT NULL,
    -- Bytes ingested by the worker (used to compute the cost via the
    -- `compute_seconds_per_gb` row in the Foundry table).
    input_bytes        BIGINT      NOT NULL DEFAULT 0,
    -- Final billed cost. Cached here so a follow-up Foundry table
    -- update never retrocedes a historical bill.
    compute_seconds    BIGINT      NOT NULL DEFAULT 0,
    persistence        TEXT        NOT NULL,
    -- TRUE when the invocation was served from the
    -- `media_set_access_pattern_outputs` cache (no runtime call,
    -- compute_seconds is 0 so the bill stays accurate).
    cache_hit          BOOLEAN     NOT NULL DEFAULT FALSE,
    invoked_by         TEXT        NOT NULL DEFAULT '',
    invoked_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_media_set_invocations_set_time
    ON media_set_access_pattern_invocations(media_set_rid, invoked_at DESC);
CREATE INDEX IF NOT EXISTS idx_media_set_invocations_kind_time
    ON media_set_access_pattern_invocations(media_set_rid, kind, invoked_at DESC);

CREATE TABLE IF NOT EXISTS media_set_access_pattern_outputs (
    -- Surrogate PK so the natural key (pattern + item + params hash)
    -- can move from CACHE_TTL → PERSIST without rewriting the row.
    id              BIGSERIAL   PRIMARY KEY,
    pattern_id      TEXT        NOT NULL REFERENCES media_set_access_patterns(id) ON DELETE CASCADE,
    item_rid        TEXT        NOT NULL,
    -- SHA-256 of the canonicalised params JSON. Lets multiple
    -- variants (e.g. resize 64×64 vs 128×128) coexist as separate
    -- cache entries on the same item.
    params_hash     TEXT        NOT NULL,
    storage_uri     TEXT        NOT NULL,
    output_mime     TEXT        NOT NULL DEFAULT '',
    bytes           BIGINT      NOT NULL DEFAULT 0,
    -- NULL = PERSIST (never expires); set for CACHE_TTL.
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (pattern_id, item_rid, params_hash)
);

CREATE INDEX IF NOT EXISTS idx_access_pattern_outputs_lookup
    ON media_set_access_pattern_outputs(pattern_id, item_rid);
CREATE INDEX IF NOT EXISTS idx_access_pattern_outputs_expiry
    ON media_set_access_pattern_outputs(expires_at)
    WHERE expires_at IS NOT NULL;
