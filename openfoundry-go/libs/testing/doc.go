// Package testingx hosts shared test utilities for OpenFoundry Go services.
//
// Mirrors the Rust `libs/testing` crate. Four families of helpers:
//
//   - containers — ephemeral Postgres via testcontainers-go.
//     Migration application is left to the caller; this package only
//     boots the container and returns a connected pgxpool.
//   - fixtures   — deterministic JWT issuance and SQL seed helpers
//     (datasets, branches, transactions, markings).
//   - mocks      — net/http/httptest wrappers for stubbing neighbour
//     services (lineage, retention, audit, catalog) — the Go analogue
//     of the Rust crate's wiremock helpers.
//   - cassandra  — single-node `cassandra:5.0` container plus a
//     connected gocql session, mirrors the Rust `cassandra` module
//     gated by the `it-cassandra` feature.
//
// All helpers are intentionally permissive (panic / t.Fatal on misuse)
// — they are test-only.
//
// Build tags: container-backed harnesses (BootPostgres, BootCassandra)
// live behind `//go:build integration` so unit-test runs do not
// require a Docker daemon. mocks and fixtures compile unconditionally.
package testingx
