// Package stores is the Go port of `libs/ontology-kernel/src/stores/*`.
//
// This package is intentionally a placeholder skeleton in iter 5: only
// the [Stores] aggregate type is declared, so that
// [github.com/openfoundry/openfoundry-go/libs/ontology-kernel.AppState]
// can mirror the Rust `pub stores: stores::Stores` field 1:1 today.
//
// Subsequent iterations will land:
//   - the trait set (ObjectStore, LinkStore, … — currently in
//     libs/storage-abstraction/repos.go)
//   - the Postgres implementations (stores/pg.rs)
//   - the in-memory mock used by handler tests (stores/mock.rs)
//
// Until those land, the [Stores] struct is empty and any handler that
// needs a concrete repo should wire it through [github.com/openfoundry/openfoundry-go/libs/ontology-kernel.AppState.DB]
// directly.
package stores

// Stores mirrors `struct stores::Stores` in the Rust crate. Field set
// is intentionally empty in this iter — see package doc.
type Stores struct{}
