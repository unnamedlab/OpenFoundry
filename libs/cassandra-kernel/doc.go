// Package cassandrakernel is the OpenFoundry Cassandra/Scylla helper —
// a minimal Go counterpart of the Rust workspace's `libs/cassandra-kernel`.
//
// What this package owns
//
//   - Cluster / Session wiring (gocql) with sane defaults for the
//     auth_runtime + ontology workloads OpenFoundry runs.
//   - Migration ledger: Apply() drops a list of CREATE TABLE IF NOT
//     EXISTS / ALTER TABLE statements, idempotent by construction.
//   - Helpers shared by the per-service adapters
//     (identity-federation's sessions, ontology object/link stores, etc.).
//
// The Rust crate exposes Repository<T> + Cassandra extensions; we port
// each per-service adapter directly (see services/<name>/internal/
// cassandra*) and let cassandra-kernel stay a small primitives lib.
//
// Implementation note: the Rust crate uses `scylla-rs`. The Go side
// uses `gocql/gocql` which talks the same CQL protocol — schema +
// query strings transfer verbatim. Both clients honour the same
// keyspace + table DDL emitted by the keyspaces-job and the per-
// service Migration arrays.
package cassandrakernel
