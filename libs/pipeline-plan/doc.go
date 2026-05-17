// Package pipelineplan defines the typed operator-plan IR that
// pipeline-build-service emits and pipeline-runner executes. Phase C.1
// of ADR-0045 — the contract that replaces the free-form `--inline-sql`
// flag the Spark-backed runner consumed.
//
// A [Plan] is a directed acyclic graph of typed [Op] nodes. Each Op
// has a [Kind] (read_table, filter, project, rename, cast, aggregate,
// union, limit, write_table) and exactly one populated per-kind config
// field. Inputs reference upstream Op.IDs; the runtime topo-sorts the
// graph and executes operators in order.
//
// This package owns the schema only — types, JSON serde, validation.
// The interpreter that consumes a Plan and executes it against
// Iceberg lives in a separate package (Phase C.2). Keeping the schema
// independent means pipeline-build-service, pipeline-runner, and any
// test harness can depend on the contract without dragging the
// Iceberg client and its dep graph (apache/iceberg-go,
// substrait-protobuf pin, gocloud, …) into their build.
//
// The operator set is intentionally narrow. Phase A's inventory
// (docs/migration/pipeline-runner-spark-to-go-inventory.md) showed
// every concrete pipeline in the repo re-expresses with these nine
// operators. `join` is deferred to v2; if a Phase 0 follow-up audit
// against production `pipeline_authoring.published_dag` rows turns
// up a pipeline that needs it, this package adds OpKindJoin without a
// wire-format break.
package pipelineplan
