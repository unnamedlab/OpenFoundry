// Package repo persists AiEventEnvelope records to Postgres and serves
// the query surface backing the ai-sink HTTP API.
//
// The Iceberg writer remains the durable analytic tier; this table is
// the hot, queryable replica used by /api/v1/ai/events.
package repo

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/envelope"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every embedded migration in lexicographic order.
// Idempotent — each file uses IF NOT EXISTS guards.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}

// ErrNotFound is the sentinel returned when a single-row lookup misses.
var ErrNotFound = errors.New("ai event not found")

// Repo is the Postgres-backed ai_events repository.
type Repo struct{ Pool *pgxpool.Pool }

// AiEventRow is the in-memory shape returned by Query/Get.
// Mirrors the ai_events table column-for-column.
type AiEventRow struct {
	EventID       uuid.UUID
	At            time.Time
	Kind          string
	RunID         *uuid.UUID
	TraceID       *string
	Producer      string
	SchemaVersion uint32
	Payload       json.RawMessage
	Envelope      json.RawMessage
	CreatedAt     time.Time
}

// Insert appends a single envelope. ON CONFLICT DO NOTHING absorbs
// replays from the at-least-once Kafka consumer.
func (r *Repo) Insert(ctx context.Context, env envelope.AiEventEnvelope) error {
	_, err := r.InsertBatch(ctx, []envelope.AiEventEnvelope{env})
	return err
}

// InsertBatch appends N envelopes in a single transaction. Returns the
// number of newly-inserted rows (duplicates are silently absorbed by
// ON CONFLICT DO NOTHING).
func (r *Repo) InsertBatch(ctx context.Context, batch []envelope.AiEventEnvelope) (int, error) {
	if len(batch) == 0 {
		return 0, nil
	}
	inserted := 0
	err := pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		for i := range batch {
			env := batch[i]
			row, err := encodeForRow(env)
			if err != nil {
				return fmt.Errorf("encode envelope[%d]: %w", i, err)
			}
			tag, err := tx.Exec(ctx, insertSQL,
				row.EventID,
				row.At,
				row.Kind,
				row.RunID,
				row.TraceID,
				row.Producer,
				row.SchemaVersion,
				[]byte(row.Payload),
				[]byte(row.Envelope),
			)
			if err != nil {
				return fmt.Errorf("insert event %s: %w", row.EventID, err)
			}
			inserted += int(tag.RowsAffected())
		}
		return nil
	})
	return inserted, err
}

const insertSQL = `
INSERT INTO ai_events (
    event_id, at, kind, run_id, trace_id, producer, schema_version,
    payload, envelope
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (event_id) DO NOTHING
`

// QueryFilter is the AND-combined filter accepted by Query.
type QueryFilter struct {
	Kind     string
	RunID    *uuid.UUID
	TraceID  string
	Producer string
	From     *time.Time
	To       *time.Time
}

// Cursor is the opaque continuation token used to page Query results.
// Sortable by (at, event_id) so the same cursor is stable across
// re-issuance.
type Cursor struct {
	At      time.Time `json:"a"`
	EventID uuid.UUID `json:"e"`
}

// Query returns up to `limit` rows matching `f`, ordered by
// (at DESC, event_id DESC). `nextCursor` is non-nil when more rows are
// available.
func (r *Repo) Query(ctx context.Context, f QueryFilter, limit int, after *Cursor) ([]AiEventRow, *Cursor, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	clauses := make([]string, 0, 7)
	args := make([]any, 0, 8)
	idx := 1
	add := func(clause string, val any) {
		clauses = append(clauses, fmt.Sprintf(clause, idx))
		args = append(args, val)
		idx++
	}
	if f.Kind != "" {
		add("kind = $%d", f.Kind)
	}
	if f.RunID != nil {
		add("run_id = $%d", *f.RunID)
	}
	if f.TraceID != "" {
		add("trace_id = $%d", f.TraceID)
	}
	if f.Producer != "" {
		add("producer = $%d", f.Producer)
	}
	if f.From != nil {
		add("at >= $%d", *f.From)
	}
	if f.To != nil {
		add("at < $%d", *f.To)
	}
	if after != nil {
		clauses = append(clauses, fmt.Sprintf("(at, event_id) < ($%d, $%d)", idx, idx+1))
		args = append(args, after.At, after.EventID)
		idx += 2
	}

	sql := selectSQL
	if len(clauses) > 0 {
		sql += " WHERE " + strings.Join(clauses, " AND ")
	}
	sql += fmt.Sprintf(" ORDER BY at DESC, event_id DESC LIMIT $%d", idx)
	args = append(args, limit+1)

	rows, err := r.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	out := make([]AiEventRow, 0, limit)
	for rows.Next() {
		row, err := scanRow(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	var next *Cursor
	if len(out) > limit {
		last := out[limit-1]
		out = out[:limit]
		next = &Cursor{At: last.At, EventID: last.EventID}
	}
	return out, next, nil
}

// Stream issues the same query as Query but yields rows through `fn`
// without buffering — used by ExportEvents to stream NDJSON.
func (r *Repo) Stream(ctx context.Context, f QueryFilter, fn func(AiEventRow) error) error {
	clauses := make([]string, 0, 6)
	args := make([]any, 0, 6)
	idx := 1
	add := func(clause string, val any) {
		clauses = append(clauses, fmt.Sprintf(clause, idx))
		args = append(args, val)
		idx++
	}
	if f.Kind != "" {
		add("kind = $%d", f.Kind)
	}
	if f.RunID != nil {
		add("run_id = $%d", *f.RunID)
	}
	if f.TraceID != "" {
		add("trace_id = $%d", f.TraceID)
	}
	if f.Producer != "" {
		add("producer = $%d", f.Producer)
	}
	if f.From != nil {
		add("at >= $%d", *f.From)
	}
	if f.To != nil {
		add("at < $%d", *f.To)
	}

	sql := selectSQL
	if len(clauses) > 0 {
		sql += " WHERE " + strings.Join(clauses, " AND ")
	}
	sql += " ORDER BY at ASC, event_id ASC"

	rows, err := r.Pool.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		row, err := scanRow(rows)
		if err != nil {
			return err
		}
		if err := fn(row); err != nil {
			return err
		}
	}
	return rows.Err()
}

const selectSQL = `
SELECT event_id, at, kind, run_id, trace_id, producer, schema_version,
    payload, envelope, created_at
FROM ai_events`

func scanRow(rows pgx.Rows) (AiEventRow, error) {
	var r AiEventRow
	var (
		payload  []byte
		envBytes []byte
		runID    *uuid.UUID
		traceID  *string
	)
	err := rows.Scan(
		&r.EventID, &r.At, &r.Kind, &runID, &traceID,
		&r.Producer, &r.SchemaVersion,
		&payload, &envBytes, &r.CreatedAt,
	)
	if err != nil {
		return AiEventRow{}, err
	}
	r.RunID = runID
	r.TraceID = traceID
	r.Payload = json.RawMessage(payload)
	r.Envelope = json.RawMessage(envBytes)
	return r, nil
}

// encodeForRow materialises the envelope into the typed column shape
// stored in ai_events. The full envelope JSON is preserved verbatim in
// the `envelope` column so consumers can recover any field the index
// columns don't materialise.
func encodeForRow(env envelope.AiEventEnvelope) (AiEventRow, error) {
	if env.EventID == uuid.Nil {
		return AiEventRow{}, errors.New("envelope missing event_id")
	}
	if env.At <= 0 {
		return AiEventRow{}, errors.New("envelope missing at")
	}
	payload := env.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("null")
	}
	full, err := json.Marshal(env)
	if err != nil {
		return AiEventRow{}, fmt.Errorf("marshal envelope: %w", err)
	}
	return AiEventRow{
		EventID:       env.EventID,
		At:            time.UnixMicro(env.At).UTC(),
		Kind:          string(env.Kind),
		RunID:         env.RunID,
		TraceID:       env.TraceID,
		Producer:      env.Producer,
		SchemaVersion: env.SchemaVersion,
		Payload:       payload,
		Envelope:      json.RawMessage(full),
	}, nil
}
