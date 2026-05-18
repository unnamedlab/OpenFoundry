// Package repo persists ActionEnvelope records to Postgres and serves
// the query surface for the action-log-sink HTTP API.
//
// The Iceberg writer remains the durable analytic tier; this table is
// the hot, queryable replica fronted by /api/v1/action-log/events*.
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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/envelope"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrNotFound is returned by Get when no row matches the requested
// event_id. Mirrors the sentinel used elsewhere in the codebase so
// callers can branch via errors.Is.
var ErrNotFound = errors.New("action_log event not found")

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

// Repo is the Postgres-backed action_log_events repository.
type Repo struct{ Pool *pgxpool.Pool }

// ActionEventRow is the in-memory shape returned by Query/Get/Stream.
// Columns are scanned 1:1 from action_log_events.
type ActionEventRow struct {
	EventID              string
	ActionTypeID         string
	ActionName           string
	ObjectTypeID         string
	ObjectID             string
	Tenant               string
	ActorSub             string
	ActorEmail           string
	OrganizationID       string
	Status               string
	Parameters           json.RawMessage
	PreviousState        json.RawMessage
	NewState             json.RawMessage
	TargetClassification string
	AppliedAtMs          int64
	KafkaTS              time.Time
	CreatedAt            time.Time
}

// Insert appends a single envelope. ON CONFLICT DO NOTHING absorbs
// replays from the at-least-once Kafka consumer.
func (r *Repo) Insert(ctx context.Context, env envelope.ActionEnvelope) error {
	_, err := r.InsertBatch(ctx, []envelope.ActionEnvelope{env})
	return err
}

// InsertBatch appends N envelopes in a single transaction. Returns the
// number of newly-inserted rows (duplicates are silently absorbed by
// ON CONFLICT DO NOTHING).
func (r *Repo) InsertBatch(ctx context.Context, batch []envelope.ActionEnvelope) (int, error) {
	if len(batch) == 0 {
		return 0, nil
	}
	inserted := 0
	err := pgx.BeginFunc(ctx, r.Pool, func(tx pgx.Tx) error {
		for i := range batch {
			env := batch[i]
			tag, err := tx.Exec(ctx, insertSQL,
				env.EventID,
				env.ActionTypeID,
				env.ActionName,
				env.ObjectTypeID,
				strOrEmpty(env.ObjectID),
				env.Tenant,
				env.ActorSub,
				strOrEmpty(env.ActorEmail),
				strOrEmpty(env.OrganizationID),
				env.Status,
				jsonbOrNull(env.Parameters),
				jsonbOrNull(env.PreviousState),
				jsonbOrNull(env.NewState),
				strOrEmpty(env.TargetClassification),
				env.AppliedAtMs,
			)
			if err != nil {
				return fmt.Errorf("insert event %s: %w", env.EventID, err)
			}
			inserted += int(tag.RowsAffected())
		}
		return nil
	})
	return inserted, err
}

const insertSQL = `
INSERT INTO action_log_events (
    event_id, action_type_id, action_name, object_type_id, object_id,
    tenant, actor_sub, actor_email, organization_id, status,
    parameters, previous_state, new_state, target_classification, applied_at_ms
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15
)
ON CONFLICT (event_id) DO NOTHING
`

// QueryFilter is the AND-combined filter accepted by Query/Stream.
type QueryFilter struct {
	Tenant       string
	ActorSub     string
	ObjectTypeID string
	ObjectID     string
	ActionName   string
	Status       string
	From         *time.Time
	To           *time.Time
}

// Cursor is the opaque continuation token used by Query.
// Sorted by (applied_at_ms DESC, event_id DESC) so the cursor is stable
// across re-issuance.
type Cursor struct {
	AppliedAtMs int64  `json:"a"`
	EventID     string `json:"e"`
}

// Get returns a single row by event_id, or ErrNotFound.
func (r *Repo) Get(ctx context.Context, eventID string) (ActionEventRow, error) {
	rows, err := r.Pool.Query(ctx, selectSQL+" WHERE event_id = $1", eventID)
	if err != nil {
		return ActionEventRow{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return ActionEventRow{}, err
		}
		return ActionEventRow{}, ErrNotFound
	}
	return scanRow(rows)
}

// Query returns up to `limit` rows matching `f`, ordered by
// (applied_at_ms DESC, event_id DESC). When more rows are available the
// returned *Cursor points just past the last row.
func (r *Repo) Query(ctx context.Context, f QueryFilter, limit int, after *Cursor) ([]ActionEventRow, *Cursor, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	sql, args := buildQuery(f, after, true, limit)

	rows, err := r.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	out := make([]ActionEventRow, 0, limit)
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
		next = &Cursor{AppliedAtMs: last.AppliedAtMs, EventID: last.EventID}
	}
	return out, next, nil
}

// Stream issues the same query as Query (ascending) and yields rows
// through `fn` without buffering — used by the NDJSON export endpoint.
func (r *Repo) Stream(ctx context.Context, f QueryFilter, fn func(ActionEventRow) error) error {
	sql, args := buildQuery(f, nil, false, 0)

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
SELECT event_id, action_type_id, action_name, object_type_id, object_id,
    tenant, actor_sub, actor_email, organization_id, status,
    parameters, previous_state, new_state, target_classification,
    applied_at_ms, kafka_ts, created_at
FROM action_log_events`

func buildQuery(f QueryFilter, after *Cursor, paginate bool, limit int) (string, []any) {
	clauses := make([]string, 0, 9)
	args := make([]any, 0, 11)
	idx := 1
	add := func(clause string, val any) {
		clauses = append(clauses, fmt.Sprintf(clause, idx))
		args = append(args, val)
		idx++
	}
	if f.Tenant != "" {
		add("tenant = $%d", f.Tenant)
	}
	if f.ActorSub != "" {
		add("actor_sub = $%d", f.ActorSub)
	}
	if f.ObjectTypeID != "" {
		add("object_type_id = $%d", f.ObjectTypeID)
	}
	if f.ObjectID != "" {
		add("object_id = $%d", f.ObjectID)
	}
	if f.ActionName != "" {
		add("action_name = $%d", f.ActionName)
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	if f.From != nil {
		add("applied_at_ms >= $%d", f.From.UnixMilli())
	}
	if f.To != nil {
		add("applied_at_ms < $%d", f.To.UnixMilli())
	}
	if after != nil {
		clauses = append(clauses, fmt.Sprintf("(applied_at_ms, event_id) < ($%d, $%d)", idx, idx+1))
		args = append(args, after.AppliedAtMs, after.EventID)
		idx += 2
	}

	sql := selectSQL
	if len(clauses) > 0 {
		sql += " WHERE " + strings.Join(clauses, " AND ")
	}
	if paginate {
		sql += fmt.Sprintf(" ORDER BY applied_at_ms DESC, event_id DESC LIMIT $%d", idx)
		args = append(args, limit+1)
	} else {
		sql += " ORDER BY applied_at_ms ASC, event_id ASC"
	}
	return sql, args
}

func scanRow(rows pgx.Rows) (ActionEventRow, error) {
	var r ActionEventRow
	var params, prev, next []byte
	err := rows.Scan(
		&r.EventID, &r.ActionTypeID, &r.ActionName, &r.ObjectTypeID, &r.ObjectID,
		&r.Tenant, &r.ActorSub, &r.ActorEmail, &r.OrganizationID, &r.Status,
		&params, &prev, &next, &r.TargetClassification,
		&r.AppliedAtMs, &r.KafkaTS, &r.CreatedAt,
	)
	if err != nil {
		return ActionEventRow{}, err
	}
	r.Parameters = json.RawMessage(params)
	r.PreviousState = json.RawMessage(prev)
	r.NewState = json.RawMessage(next)
	return r, nil
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// jsonbOrNull turns the publisher's JSON-encoded-string nullable fields
// into a JSONB column value: nil → SQL NULL; non-nil → parsed JSON when
// the string is valid JSON, else the raw string wrapped as a JSON string
// so the column always holds well-formed JSONB.
func jsonbOrNull(s *string) any {
	if s == nil {
		return nil
	}
	if json.Valid([]byte(*s)) {
		return []byte(*s)
	}
	wrapped, _ := json.Marshal(*s)
	return wrapped
}
