// Package pipelineruntime executes a typed [pipelineplan.Plan] against
// a [Reader]/[Writer] pair. Phase C.2 of ADR-0045 — the interpreter
// that replaces the Spark `df.writeTo(...).createOrReplace()` path.
//
// Scope of this package:
//
//   - The nine v1 operators (read_table, filter, project, rename,
//     cast, aggregate, union, limit, write_table) implemented as
//     pure functions over RowStream.
//   - Topological [Executor] that walks a validated Plan, wires
//     upstream RowStreams into each operator and drives the terminal
//     write_table.
//   - Adapter to libs/pipeline-expression for the DSL strings inside
//     filter and project ops (parse → eval → typed value).
//
// Out of scope of this package (lives elsewhere on purpose so the
// schema and the interpreter do not drag the Iceberg client graph):
//
//   - The concrete [Reader] backed by apache/iceberg-go (Phase A
//     pattern, including the substrait-protobuf pin and the
//     `_ "github.com/apache/iceberg-go/io/gocloud"` blank import
//     plus the `s3.remote-signing-enabled=false` Lakekeeper
//     compat override). Ships in Phase C.5 with the runner wiring.
//   - The concrete [Writer] that posts to the OpenFoundry Iceberg
//     HTTP append adapter (Phase B pattern). Also Phase C.5.
//   - Pipeline composition / orchestration (build-service emits
//     Plan, runner consumes Plan). Phases C.4 and C.5.
//
// Memory model: all operators stream row-at-a-time except aggregate,
// which has to materialise the per-group state. v1 targets the PoC
// scale (≤500 rows) and uses on-line single-pass accumulators
// (Welford for stddev) so the only memory cost is one accumulator
// per group, not the raw rows.
package pipelineruntime
