package writer

import (
	"context"

	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/envelope"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/repo"
)

// PostgresWriter is the hot-store sink. It appends each batch to the
// action_log_events table behind the action-log-sink HTTP API.
//
// Iceberg remains the durable analytic tier; PostgresWriter is used
// either standalone (queryable-only deployments) or composed with the
// Iceberg writer via MultiWriter so both tiers receive every batch.
type PostgresWriter struct {
	Repo *repo.Repo
}

// NewPostgresWriter wires a PostgresWriter against `r`.
func NewPostgresWriter(r *repo.Repo) *PostgresWriter { return &PostgresWriter{Repo: r} }

// Append inserts the batch via Repo.InsertBatch. Empty batches are a
// no-op (the runtime guards against this but a guard here keeps the
// writer composable with MultiWriter without surfacing ErrEmptyBatch).
func (p *PostgresWriter) Append(ctx context.Context, batch []envelope.ActionEnvelope) error {
	if len(batch) == 0 {
		return nil
	}
	_, err := p.Repo.InsertBatch(ctx, batch)
	return err
}

// Close is a no-op — the pool lifecycle is owned by the caller.
func (p *PostgresWriter) Close() error { return nil }

// MultiWriter fans each batch out to every wrapped Writer in order.
// The first error short-circuits the call: at-least-once delivery means
// the runtime keeps the batch and retries on the next iteration, so a
// later success would not undo a partial-batch failure.
type MultiWriter struct {
	Writers []Writer
}

// NewMultiWriter constructs a MultiWriter from one or more Writers.
func NewMultiWriter(ws ...Writer) *MultiWriter { return &MultiWriter{Writers: ws} }

// Append forwards the batch to every wrapped writer, stopping on the
// first error.
func (m *MultiWriter) Append(ctx context.Context, batch []envelope.ActionEnvelope) error {
	for _, w := range m.Writers {
		if err := w.Append(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}

// Close closes every wrapped writer, returning the first non-nil error
// (subsequent close errors are best-effort).
func (m *MultiWriter) Close() error {
	var firstErr error
	for _, w := range m.Writers {
		if err := w.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
