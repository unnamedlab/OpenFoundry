// Package adapters defines the per-connector adapter contract that the
// connector-management-service uses to dispatch discovery, virtual-table
// preview, Arrow IPC streaming, and ingest-spec construction to a specific
// data source (BigQuery, Snowflake, Kafka, …).
//
// The package mirrors the dispatcher surface that lives in
// `services/connector-management-service/src/domain/discovery.rs` plus the
// per-connector module API in `src/connectors/*.rs`. Each Rust module
// exposes (at most) four capabilities — discover, query_virtual_table,
// stream_arrow, build_ingest_spec — and the Go side collapses them into the
// [ConnectorAdapter] interface defined in adapter.go.
//
// Lifecycle: per-connector packages register a [Factory] with the package-
// level [Registry] in their package init. Callers resolve the factory by
// connector_type and ask it for an adapter instance per request, which lets
// adapters hold per-connection state (HTTP clients, driver pools, …)
// without the registry having to know about it.
package adapters

import "errors"

// ErrNotImplemented is returned by adapters whose capability has not been
// wired yet — either skeleton stubs (CMA-14) or per-connector slices that
// only land partial coverage. Callers can errors.Is() against this to
// translate the failure into HTTP 501 / a "not supported for connector type"
// envelope, keeping the response shape identical to Rust's
// `format!("zero-copy is not supported for connector type: {other}")` /
// `"discover is not supported for connector type: {other}"` errors.
var ErrNotImplemented = errors.New("adapter capability not implemented")

// ErrAdapterNotFound is returned by [Registry.Get] / [Registry.Lookup] when
// no factory has been registered for a connector_type. The Rust dispatcher
// in `domain/discovery.rs` falls through to a string-formatted error for
// the same case; callers that need byte-identical wire output should wrap
// this with the relevant `discover is not supported …` / `zero-copy is not
// supported …` prefix.
var ErrAdapterNotFound = errors.New("no adapter registered for connector type")

// ErrAlreadyRegistered is returned by [Registry.Register] when a
// connector_type has already been bound to a factory. Re-registration is a
// programmer error (typically a duplicate package init in tests); the
// registry refuses it instead of silently overwriting so that ordering
// surprises surface immediately.
var ErrAlreadyRegistered = errors.New("adapter already registered for connector type")
