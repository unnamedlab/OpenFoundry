// Package ontologykernel is the Go port of the Rust crate
// `libs/ontology-kernel`. It hosts the shared ontology domain, models,
// and HTTP handler layer reused by every ontology-* and
// object-database-service binary.
//
// Migration status is tracked in
// openfoundry-go/ONTOLOGY-KERNEL-MIGRATION.md. The Go layout mirrors
// the Rust crate verbatim:
//
//	libs/ontology-kernel/
//	├── doc.go               (this file)
//	├── appstate.go          (mirrors Rust src/lib.rs `AppState`)
//	├── config/              (src/config.rs)
//	├── metrics/             (src/metrics.rs)
//	├── models/              (src/models/*)
//	├── domain/              (src/domain/*)
//	├── handlers/            (src/handlers/*)
//	└── stores/              (src/stores/*)
//
// Wire-compat is the prime invariant: every JSON shape, default value,
// enum token, and ordering rule has a Go test that pins it against the
// matching Rust assertion.
package ontologykernel
