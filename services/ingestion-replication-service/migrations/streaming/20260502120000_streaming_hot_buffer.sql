-- D1.1.2 Bloque B1 — hot buffer columns on streaming_streams.
--
-- * `partitions` controls how many Kafka partitions the topic is created
--   with by `HotBuffer::ensure_topic`. NATS topics ignore the value but
--   we still record it so the API contract is uniform across backends.
-- * `consistency_guarantee` is the publish-side delivery contract the
--   caller asks for. The backend chooses the matching producer settings:
--     - `at-least-once` (default): producer waits for broker ack.
--     - `exactly-once`:           transactional producer + idempotent.
--     - `at-most-once`:           fire-and-forget (no broker ack wait).
ALTER TABLE streaming_streams
    ADD COLUMN IF NOT EXISTS partitions INTEGER NOT NULL DEFAULT 3
        CHECK (partitions BETWEEN 1 AND 1024),
    ADD COLUMN IF NOT EXISTS consistency_guarantee TEXT NOT NULL DEFAULT 'at-least-once'
        CHECK (consistency_guarantee IN ('at-most-once','at-least-once','exactly-once'));
