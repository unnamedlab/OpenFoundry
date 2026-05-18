package writer

import (
	"context"

	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/envelope"
	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/repo"
)

// PostgresWriter is the hot-store sink. It appends each per-table batch
// to the `ai_events` table behind the ai-sink query surface.
//
// Iceberg remains the durable analytic tier; this writer is used either
// standalone (queryable-only deployments) or composed with the Iceberg
// writer via MultiWriter so both tiers receive every batch.
type PostgresWriter struct {
	Repo *repo.Repo
}

func NewPostgresWriter(r *repo.Repo) *PostgresWriter { return &PostgresWriter{Repo: r} }

// Append flattens the per-table map into a single transactional insert
// against ai_events. The destination table is implied by `kind` — all
// four envelope kinds share one Postgres table by design.
func (p *PostgresWriter) Append(ctx context.Context, byTable map[string][]envelope.AiEventEnvelope) error {
	total := 0
	for _, group := range byTable {
		total += len(group)
	}
	if total == 0 {
		return nil
	}
	flat := make([]envelope.AiEventEnvelope, 0, total)
	for _, group := range byTable {
		flat = append(flat, group...)
	}
	_, err := p.Repo.InsertBatch(ctx, flat)
	return err
}

func (p *PostgresWriter) Close() error { return nil }

// MultiWriter fans each batch out to every wrapped Writer in order.
// First error short-circuits — at-least-once guarantees that a later
// success after a partial-batch failure is the supervisor's job.
type MultiWriter struct {
	Writers []Writer
}

func NewMultiWriter(ws ...Writer) *MultiWriter { return &MultiWriter{Writers: ws} }

func (m *MultiWriter) Append(ctx context.Context, byTable map[string][]envelope.AiEventEnvelope) error {
	for _, w := range m.Writers {
		if err := w.Append(ctx, byTable); err != nil {
			return err
		}
	}
	return nil
}

func (m *MultiWriter) Close() error {
	var firstErr error
	for _, w := range m.Writers {
		if err := w.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
