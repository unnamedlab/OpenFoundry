-- Runtime cutover: hot events, checkpoint offsets/history, and cold
-- archive tracking moved out of the Postgres runtime.
--
-- Postgres remains the control plane for stream/topology/window
-- metadata plus run history. The hot path now lives in the service
-- runtime store (memory + optional Cassandra metadata).

DROP TABLE IF EXISTS streaming_topology_checkpoints;
DROP TABLE IF EXISTS streaming_cold_archives;
DROP TABLE IF EXISTS streaming_checkpoints;
DROP TABLE IF EXISTS streaming_events;
