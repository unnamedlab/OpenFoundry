-- 000_keyspace.cql — actions_log keyspace
-- Owner: ontology-actions-service
-- Single TWCS table backing actions, side-effects, revisions,
-- funnel runs and rule runs.

CREATE KEYSPACE IF NOT EXISTS actions_log
  WITH replication = {
    'class': 'NetworkTopologyStrategy',
    'dc1': 3,
    'dc2': 3,
    'dc3': 3
  }
  AND durable_writes = true;
