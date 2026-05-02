// Package activities hosts placeholder bindings for the activities
// that scan Cassandra and publish to Kafka. The actual activity
// bodies live on the Rust side and are invoked over gRPC; this
// stub keeps the worker registration buildable until the Rust
// side ships.
package activities

import "context"

// Activities is the receiver registered with the Temporal worker.
// Activity method names are pinned in
// `internal/contract/contract.go` and must match byte-for-byte.
type Activities struct{}

// ScanCassandraObjects is a substrate stub. The real implementation
// proxies to the Rust `ontology-indexer` admin endpoint or the
// Cassandra repo directly. Returning an empty page makes the
// workflow terminate cleanly so the worker can be registered before
// the Rust side is wired.
func (a *Activities) ScanCassandraObjects(ctx context.Context, in any) (map[string]any, error) {
	return map[string]any{
		"records":    []any{},
		"next_token": "",
	}, nil
}

// PublishReindexBatch is a substrate stub. Real implementation
// publishes to `ontology.reindex.v1` via `event-bus-data` (Rust)
// or franz-go (Go). Both are acceptable; Rust is preferred so the
// `OutboxEvent::event_id` derivation lives in one place.
func (a *Activities) PublishReindexBatch(ctx context.Context, in any) (map[string]any, error) {
	return map[string]any{"published": int64(0)}, nil
}
