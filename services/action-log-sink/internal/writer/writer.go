// Package writer abstracts the action-log-sink output target.
//
// Two implementations:
//
//   - IcebergWriter — production path. Posts the per-batch
//     `lakekeeper.default.action_log` rows to the OpenFoundry Iceberg
//     HTTP append adapter (`POST /openfoundry/iceberg/v1/append`,
//     served by iceberg-catalog-service). The adapter is responsible
//     for writing the Parquet data file and committing the Iceberg
//     snapshot atomically. Same pattern services/audit-sink and
//     services/ai-sink already use; apache/iceberg-go's write-side is
//     not stable enough for direct use (see ADR-0045 Phase B and the
//     write-side note in services/audit-sink/README.md).
//   - JSONLWriter — append batches as newline-delimited JSON to a
//     local file (or stdout when path is "-"). Dev/staging fallback
//     selected explicitly via env (ACTION_LOG_SINK_JSONL_PATH).
//
// The Writer interface is intentionally narrow: append + close.
// Batching policy and Kafka offset commits live in `internal/runtime`.
package writer

import (
	"context"
	"errors"

	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/envelope"
)

// Writer is the action-log-sink output target.
type Writer interface {
	// Append durably writes `batch` and returns when it is safe for
	// the caller to commit Kafka offsets. An error here means the
	// runtime must NOT commit offsets — the batch will replay.
	Append(ctx context.Context, batch []envelope.ActionEnvelope) error
	// Close flushes pending data and releases resources. Safe to
	// call multiple times.
	Close() error
}

// ErrEmptyBatch is returned when Append receives a zero-length slice.
// The runtime guards against this before calling.
var ErrEmptyBatch = errors.New("action-log append batch is empty")

// ErrTableNotFound surfaces a 404 from the HTTP adapter — the
// `lakekeeper.default.action_log` table has not been created yet (see
// the DDL in infra/dev/action-log-sink.yaml header comment).
var ErrTableNotFound = errors.New("action_log iceberg table not found")

// ErrSchemaMismatch surfaces 409/422 — the row shape sent does not
// match the table the catalog has. Mirrors audit-sink semantics.
var ErrSchemaMismatch = errors.New("action_log iceberg table schema mismatch")

// ErrCommitFailed is the umbrella for transport failures and any
// other non-2xx response from the HTTP adapter.
var ErrCommitFailed = errors.New("action_log iceberg commit failed")
