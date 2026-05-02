-- 000_keyspace.cql — ontology_objects keyspace
-- Owner: object-database-service
-- See docs/architecture/ontology-cassandra-tables.md §1

CREATE KEYSPACE IF NOT EXISTS ontology_objects
  WITH replication = {
    'class': 'NetworkTopologyStrategy',
    'dc1': 3,
    'dc2': 3,
    'dc3': 3
  }
  AND durable_writes = true;
