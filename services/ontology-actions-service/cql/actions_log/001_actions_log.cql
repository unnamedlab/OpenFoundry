-- 001_actions_log.cql — append-only action / revision / run log
-- Drives:
--   * action_executions writes (handlers/actions.rs:3559)
--   * action_execution_side_effects (handlers/actions.rs:4687)
--   * object_revisions (handlers/objects.rs:2656, handlers/bindings.rs:572)
--   * ontology_funnel_runs (handlers/funnel.rs:1179)
--   * ontology_rule_runs (rule engine)
-- Discriminated by `kind` column.
-- TTL 90 d (7 776 000 s); TWCS window 1 day = SSTables drop atomically.

CREATE TABLE IF NOT EXISTS actions_log.actions_log (
    tenant            text,
    day_bucket        date,
    applied_at        timestamp,
    action_id         timeuuid,
    kind              text,                 -- 'action' | 'side_effect' | 'revision' | 'funnel_run' | 'rule_run'
    actor_id          uuid,
    target_object_id  timeuuid,
    target_type_id    text,
    payload           text,                 -- canonical JSON (event body)
    status            text,
    failure_type      text,
    duration_ms       int,
    event_id          text,
    PRIMARY KEY ((tenant, day_bucket), applied_at, action_id)
)
WITH CLUSTERING ORDER BY (applied_at DESC, action_id ASC)
 AND compaction = {
    'class': 'TimeWindowCompactionStrategy',
    'compaction_window_unit': 'DAYS',
    'compaction_window_size': 1
 }
 AND default_time_to_live = 7776000   -- 90 days
 AND gc_grace_seconds = 10800          -- 3 h: TWCS doesn't need long grace
 AND comment = 'S1.1.b — append-only log for actions, revisions, funnel runs, rule runs. TTL 90 d.';
