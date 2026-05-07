// Package domain is the Go port of `libs/ontology-kernel/src/domain/*`.
//
// Each file mirrors a single Rust file 1:1 in function signatures,
// error messages, and SQL shapes. Wire-compat is the prime invariant:
// every JSON shape, error string, default value and ordering rule has
// a Go test pinning it against the matching Rust assertion.
//
// The Rust source is split into ~30 files; the Go port lands in
// reviewable iter slices grouped by concern (types base, repositories,
// rules + traversal, search). Each iter advances the migration tracker
// in openfoundry-go/ONTOLOGY-KERNEL-MIGRATION.md.
package domain
