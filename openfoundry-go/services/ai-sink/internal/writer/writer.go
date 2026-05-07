// Package writer abstracts the ai-sink output target.
//
// Two implementations:
//
//   - IcebergWriter — production path. It targets an explicit
//     OpenFoundry Iceberg table-writer adapter that performs the
//     Parquet data-file write plus Iceberg snapshot commit, one append
//     per non-empty table group. The adapter is used because iceberg-go
//     still lacks a stable write-side API matching Rust's
//     `append_record_batches`.
//   - JSONLWriter — opens one `<table>.jsonl` per Iceberg table inside
//     a directory. This remains a dev/staging fallback selected
//     explicitly by env.
package writer

import (
	"context"
	"errors"

	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/envelope"
)

// Writer is the per-table batch sink. Implementations group the input
// batch by `table` and durably persist each group before returning.
type Writer interface {
	// Append durably writes `byTable`. Keys are Iceberg table names,
	// values are the per-table batches. Returns when it is safe for
	// the caller to commit Kafka offsets.
	Append(ctx context.Context, byTable map[string][]envelope.AiEventEnvelope) error
	// Close releases resources. Safe to call multiple times.
	Close() error
}

// ErrNotImplemented is kept for older callers that still branch on the
// former stub behavior. IcebergWriter no longer returns this error.
var ErrNotImplemented = errors.New("writer not implemented in this build")
