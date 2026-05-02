-- 002_actions_by_object.cql — object-scoped action history index
-- Drives: ActionLogStore::list_for_object without scanning tenant/day buckets.

ALTER TABLE actions_log.actions_log ADD IF NOT EXISTS subject text;

CREATE TABLE IF NOT EXISTS actions_log.actions_by_object (
    tenant            text,
    target_object_id  timeuuid,
    applied_at        timestamp,
    action_id         timeuuid,
    kind              text,
    actor_id          uuid,
    subject           text,
    payload           text,
    PRIMARY KEY ((tenant, target_object_id), applied_at, action_id)
)
WITH CLUSTERING ORDER BY (applied_at DESC, action_id ASC)
 AND compaction = {
    'class': 'TimeWindowCompactionStrategy',
    'compaction_window_unit': 'DAYS',
    'compaction_window_size': 7
 }
 AND default_time_to_live = 7776000
 AND gc_grace_seconds = 10800
 AND comment = 'S1.4 — object-scoped action history index. TTL 90 d.';
