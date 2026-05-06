-- 000_keyspace.cql — ontology_indexes keyspace
-- Owner: object-database-service
-- Holds materialised link tables (outgoing + incoming).

CREATE KEYSPACE IF NOT EXISTS ontology_indexes
  WITH replication = {
    'class': 'NetworkTopologyStrategy',
    'dc1': 3,
    'dc2': 3,
    'dc3': 3
  }
  AND durable_writes = true;
