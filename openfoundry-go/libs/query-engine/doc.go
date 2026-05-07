// Package queryengine ports `libs/query-engine/src/lib.rs` to Go.
//
// The Rust crate is a thin wrapper around DataFusion that
// sql-bi-gateway-service uses for local SQL execution (the path that
// runs `SELECT 1`-style probes from BI clients before any backend is
// federated). Go has no production-quality DataFusion equivalent, so
// this package provides a deliberately-minimal substitute: a literal
// evaluator for the `SELECT <expr-list>` shape used by BI client
// probes, returning Apache Arrow record batches.
//
// What it handles
//
//	SELECT 1                  -> int64 [1] in one column
//	SELECT 1, 2, 3            -> three int64 columns, one row each
//	SELECT 1 + 1              -> int64 [2]
//	SELECT 1.5 * 2            -> float64 [3.0]
//	SELECT 'hello'            -> utf8  ["hello"]
//	SELECT TRUE / FALSE       -> bool  [true|false]
//	SELECT NULL               -> null array (logical type "null")
//
// What it rejects (returns [ErrUnsupportedLocalExecution])
//
//	SELECT * FROM <anything>  — needs a registered catalog
//	WHERE / GROUP BY / JOIN   — needs a planner
//	subqueries, CTEs          — same
//
// In production the gateway runs with `WAREHOUSING_FLIGHT_SQL_URL`
// set, and statements that don't match the literal-SELECT subset get
// forwarded to sql-warehousing-service over Flight SQL rather than
// being executed here. The literal evaluator exists so BI-client
// connection probes (Tableau, Superset, JDBC) succeed even when no
// warehousing endpoint is configured — matching the Rust behaviour
// where DataFusion folds `SELECT 1` into a single int64 batch
// without touching any catalog.
package queryengine
