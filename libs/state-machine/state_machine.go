// Package statemachine is the Postgres-backed state-machine helper
// for OpenFoundry's Foundry-pattern orchestration substrate
// (ADR-0037). Mirrors libs/state-machine/src/lib.rs verbatim — same
// Loaded / PgStore / WithRetry contract, same SQL shape, same column
// layout (id / state / state_data / version / expires_at /
// created_at / updated_at).
//
// Each consumer service owns the state-machine table inside its own
// schema; this package handles the optimistic-concurrency UPDATE +
// JSON serialisation + timeout sweep + retry plumbing so the domain
// code stays focused on the pure transition function.
package statemachine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Aggregate is the contract the consumer's machine type must satisfy.
// Mirrors the Rust StateMachine trait: pure transition logic +
// stable-id + queryable state + optional timeout deadline. The
// machine is JSON-serialised via encoding/json into the state_data
// column, so every implementation must be encodable.
//
// The pointer-receiver Apply pattern is the Go-idiomatic translation
// of the Rust consuming `fn transition(self, event) → Result<Self>`:
// the store decodes a fresh copy, calls Apply, and re-encodes. The
// implementor's Apply MUST NOT touch shared state; it only mutates
// the machine in place.
type Aggregate[E any] interface {
	// Apply runs the pure transition or returns TransitionError when
	// the event isn't valid in the current state.
	Apply(event E) error
	// CurrentState renders the discriminator into the queryable
	// `state` column. Use a stable wire string (e.g. "pending") —
	// the operator dashboards lean on it.
	CurrentState() string
	// AggregateID is the primary key.
	AggregateID() uuid.UUID
	// ExpiresAt is the optional timeout deadline. Nil means the row
	// will never be picked up by TimeoutSweep.
	ExpiresAt() *time.Time
}

// TransitionError signals that an event isn't applicable in the
// current state. Wrap with fmt.Errorf for context.
type TransitionError struct {
	Message string
}

func (e *TransitionError) Error() string {
	return "invalid transition: " + e.Message
}

// InvalidTransition returns a freshly-allocated TransitionError —
// matches Rust's TransitionError::invalid().
func InvalidTransition(message string) error {
	return &TransitionError{Message: message}
}

// IsTransitionError reports whether err (or any wrapped err) is a
// TransitionError.
func IsTransitionError(err error) bool {
	var t *TransitionError
	return errors.As(err, &t)
}

// StaleError signals an optimistic-concurrency conflict — the row
// was modified between Load and Apply. Caller should reload and
// retry (see WithRetry).
type StaleError struct {
	ID              uuid.UUID
	ExpectedVersion int64
}

func (e *StaleError) Error() string {
	return fmt.Sprintf("stale write: row id=%s expected version=%d", e.ID, e.ExpectedVersion)
}

// IsStale reports whether err (or any wrapped err) is a StaleError.
func IsStale(err error) bool {
	var s *StaleError
	return errors.As(err, &s)
}

// NotFoundError is returned by Load when no row matches the id.
type NotFoundError struct {
	ID uuid.UUID
}

func (e *NotFoundError) Error() string {
	return "not found: id=" + e.ID.String()
}

// IsNotFound reports whether err (or any wrapped err) is a
// NotFoundError.
func IsNotFound(err error) bool {
	var n *NotFoundError
	return errors.As(err, &n)
}

// InvalidStateError is returned when the persisted state_data column
// fails to decode into the implementor's machine type — indicates
// schema drift between code and data.
type InvalidStateError struct {
	ID      uuid.UUID
	Message string
}

func (e *InvalidStateError) Error() string {
	return "invalid persisted state for id=" + e.ID.String() + ": " + e.Message
}

// Loaded bundles a freshly-decoded machine with its optimistic-
// concurrency version token. Round-trip the same Loaded (or a
// freshly-reloaded one) into Apply for the version guard to fire.
type Loaded[M any] struct {
	Machine M
	Version int64
}

// PgStore is the Postgres-backed store for one machine type. The
// table name is interpolated directly into SQL — pass a constant or
// a value validated at boot. Cheap to share or clone.
type PgStore[M Aggregate[E], E any] struct {
	pool    *pgxpool.Pool
	table   string
	newZero func() M // factory for an empty machine to decode into
}

// NewPgStore builds a store bound to `table`. `newZero` is the
// factory called by Load / TimeoutSweep to create a fresh machine
// before decoding state_data into it. Typically `func() *MyMachine
// { return &MyMachine{} }`.
func NewPgStore[M Aggregate[E], E any](pool *pgxpool.Pool, table string, newZero func() M) *PgStore[M, E] {
	return &PgStore[M, E]{pool: pool, table: table, newZero: newZero}
}

// Insert persists a fresh machine. Fails when a row with the same
// aggregate id already exists — use Load + Apply to update.
func (s *PgStore[M, E]) Insert(ctx context.Context, machine M) (Loaded[M], error) {
	state := machine.CurrentState()
	payload, err := json.Marshal(machine)
	if err != nil {
		return Loaded[M]{}, fmt.Errorf("serialize state: %w", err)
	}
	expiresAt := machine.ExpiresAt()
	id := machine.AggregateID()

	sql := fmt.Sprintf(
		`INSERT INTO %s (id, state, state_data, version, expires_at, created_at, updated_at)
         VALUES ($1, $2, $3, 1, $4, now(), now())
         RETURNING version`, s.table)
	var version int64
	if err := s.pool.QueryRow(ctx, sql, id, state, payload, expiresAt).Scan(&version); err != nil {
		return Loaded[M]{}, err
	}
	return Loaded[M]{Machine: machine, Version: version}, nil
}

// Load reads the machine + version for id. Returns NotFoundError
// when no row matches.
func (s *PgStore[M, E]) Load(ctx context.Context, id uuid.UUID) (Loaded[M], error) {
	sql := fmt.Sprintf(
		`SELECT state_data, version FROM %s WHERE id = $1`, s.table)
	var (
		payload []byte
		version int64
	)
	err := s.pool.QueryRow(ctx, sql, id).Scan(&payload, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return Loaded[M]{}, &NotFoundError{ID: id}
	}
	if err != nil {
		return Loaded[M]{}, err
	}
	machine := s.newZero()
	if err := json.Unmarshal(payload, machine); err != nil {
		return Loaded[M]{}, &InvalidStateError{ID: id, Message: err.Error()}
	}
	return Loaded[M]{Machine: machine, Version: version}, nil
}

// Apply runs the implementor's transition then persists with the
// optimistic-concurrency UPDATE. A zero-row return raises
// StaleError so the caller can reload and retry (or rely on
// WithRetry).
func (s *PgStore[M, E]) Apply(ctx context.Context, loaded Loaded[M], event E) (Loaded[M], error) {
	machine := loaded.Machine
	id := machine.AggregateID()
	if err := machine.Apply(event); err != nil {
		return Loaded[M]{}, err
	}
	state := machine.CurrentState()
	payload, err := json.Marshal(machine)
	if err != nil {
		return Loaded[M]{}, fmt.Errorf("serialize state: %w", err)
	}
	expiresAt := machine.ExpiresAt()

	sql := fmt.Sprintf(
		`UPDATE %s
            SET state = $1, state_data = $2, version = version + 1,
                expires_at = $3, updated_at = now()
          WHERE id = $4 AND version = $5
          RETURNING version`, s.table)
	var newVersion int64
	err = s.pool.QueryRow(ctx, sql, state, payload, expiresAt, id, loaded.Version).Scan(&newVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return Loaded[M]{}, &StaleError{ID: id, ExpectedVersion: loaded.Version}
	}
	if err != nil {
		return Loaded[M]{}, err
	}
	return Loaded[M]{Machine: machine, Version: newVersion}, nil
}

// TimeoutSweep returns every row whose expires_at <= now. Callers
// re-issue the appropriate timeout event via Apply so the version
// guard still fires against concurrent writers.
func (s *PgStore[M, E]) TimeoutSweep(ctx context.Context, now time.Time) ([]Loaded[M], error) {
	sql := fmt.Sprintf(
		`SELECT id, state_data, version FROM %s
          WHERE expires_at IS NOT NULL AND expires_at <= $1
          ORDER BY expires_at`, s.table)
	rows, err := s.pool.Query(ctx, sql, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Loaded[M], 0)
	for rows.Next() {
		var (
			id      uuid.UUID
			payload []byte
			version int64
		)
		if err := rows.Scan(&id, &payload, &version); err != nil {
			return nil, err
		}
		machine := s.newZero()
		if err := json.Unmarshal(payload, machine); err != nil {
			return nil, &InvalidStateError{ID: id, Message: err.Error()}
		}
		out = append(out, Loaded[M]{Machine: machine, Version: version})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// WithRetry runs op up to maxAttempts times, retrying only on
// StaleError with capped exponential backoff (initial baseDelay,
// doubled on every retry, max 1s). The closure receives the
// 1-based attempt number so it can Load fresh every time.
//
// Mirrors fn with_retry. Note: the Rust impl uses tokio sleep; we
// use time.Sleep here, which honours ctx via the caller checking
// ctx.Done() between attempts.
func WithRetry[T any](ctx context.Context, maxAttempts uint32, baseDelay time.Duration, op func(attempt uint32) (T, error)) (T, error) {
	var zero T
	if maxAttempts == 0 {
		return zero, errors.New("maxAttempts must be > 0")
	}
	delay := baseDelay
	maxDelay := time.Second

	for attempt := uint32(1); attempt <= maxAttempts; attempt++ {
		out, err := op(attempt)
		if err == nil {
			return out, nil
		}
		if !IsStale(err) || attempt == maxAttempts {
			return zero, err
		}
		// Jittered sleep: delay ± up to 30%.
		jitter := time.Duration(rand.Int63n(int64(delay) / 3))
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay + jitter):
		}
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
	return zero, fmt.Errorf("retry exhausted after %d attempts", maxAttempts)
}
