-- S1.4 action-log idempotency + read model.
-- Drives: deterministic append/retry via event_id and tenant/action reads
-- without scanning tenant/day buckets.

ALTER TABLE actions_log.actions_log ADD IF NOT EXISTS event_id text;
ALTER TABLE actions_log.actions_by_object ADD IF NOT EXISTS event_id text;

CREATE TABLE IF NOT EXISTS actions_log.actions_by_action (
    tenant            text,
    action_id         timeuuid,
    day_bucket        date,
    applied_at        timestamp,
    event_id          text,
    kind              text,
    actor_id          uuid,
    subject           text,
    target_object_id  timeuuid,
    payload           text,
    PRIMARY KEY ((tenant, action_id, day_bucket), applied_at, event_id)
)
WITH CLUSTERING ORDER BY (applied_at DESC, event_id ASC)
 AND compaction = {
    'class': 'TimeWindowCompactionStrategy',
    'compaction_window_unit': 'DAYS',
    'compaction_window_size': 1
 }
 AND default_time_to_live = 7776000
 AND gc_grace_seconds = 10800
 AND comment = 'S1.4 — action-scoped action history index. TTL 90 d.';

CREATE TABLE IF NOT EXISTS actions_log.actions_by_event (
    tenant            text,
    event_id          text,
    action_id         timeuuid,
    kind              text,
    actor_id          uuid,
    subject           text,
    target_object_id  timeuuid,
    payload           text,
    applied_at        timestamp,
    day_bucket        date,
    PRIMARY KEY ((tenant, event_id))
)
WITH default_time_to_live = 7776000
 AND gc_grace_seconds = 10800
 AND comment = 'S1.4 — deterministic action-log event idempotency ledger. TTL 90 d.';
