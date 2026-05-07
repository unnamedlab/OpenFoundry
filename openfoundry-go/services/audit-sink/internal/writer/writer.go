// Package writer abstracts the audit-sink output target.
//
// Two implementations:
//
//   - IcebergWriter — production path. It targets an explicit
//     OpenFoundry Iceberg table-writer adapter that performs the
//     Parquet data-file write plus Iceberg snapshot commit. The adapter
//     is used because iceberg-go still lacks a stable write-side API
//     matching Rust's `append_record_batches`.
//   - JSONLWriter — append batches as newline-delimited JSON to a
//     local file (or stdout when path is "-"). This remains a
//     dev/staging fallback selected explicitly by env.
//
// The Writer interface is intentionally narrow: append + close. Batch
// policy + Kafka offset commits live in `internal/runtime`.
package writer

import (
	"context"
	"errors"

	"github.com/openfoundry/openfoundry-go/services/audit-sink/internal/envelope"
)

// Writer is the audit-sink output target.
type Writer interface {
	// Append durably writes `batch` and returns when it is safe for
	// the caller to commit Kafka offsets.
	Append(ctx context.Context, batch []envelope.AuditEnvelope) error
	// Close flushes pending data and releases resources. Safe to
	// call multiple times.
	Close() error
}

// ErrNotImplemented is kept for older callers that still branch on the
// former stub behavior. IcebergWriter no longer returns this error.
var ErrNotImplemented = errors.New("writer not implemented in this build")
