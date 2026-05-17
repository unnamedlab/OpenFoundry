package signingkeys

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store is the persistence contract Manager talks to. Two impls
// ship: PostgresStore for production, InMemoryStore for tests.
type Store interface {
	EnsureSchema(ctx context.Context) error
	Active(ctx context.Context) (*Record, error)
	Retiring(ctx context.Context, now time.Time) ([]Record, error)
	All(ctx context.Context) ([]Record, error)
	Insert(ctx context.Context, rec Record) error
	Rotate(ctx context.Context, previousKid string, next Record, retireAt time.Time) error
	MarkExpired(ctx context.Context, now time.Time) (int, error)
}

// ─── Postgres impl ─────────────────────────────────────────────────────

// PostgresStore is the production Store backed by jwt_signing_keys.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wraps a pgxpool.Pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

const signingKeysSchemaDDL = `CREATE TABLE IF NOT EXISTS jwt_signing_keys (
    kid              TEXT PRIMARY KEY,
    algorithm        TEXT NOT NULL,
    public_key_pem   TEXT NOT NULL,
    private_key_enc  BYTEA NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    not_before       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    not_after        TIMESTAMPTZ NOT NULL,
    status           TEXT NOT NULL CHECK (status IN ('active', 'retiring', 'retired'))
)`

const signingKeysIndexDDL = `CREATE INDEX IF NOT EXISTS jwt_signing_keys_status_idx
    ON jwt_signing_keys (status, not_after DESC)`

const signingKeyColumns = `kid, algorithm, public_key_pem, private_key_enc,
                           created_at, not_before, not_after, status`

// EnsureSchema is idempotent — the migration already creates the
// table; this is the safety net for callers that boot without the
// migrator (e.g. integration tests that bring up a stock Postgres).
func (s *PostgresStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, signingKeysSchemaDDL); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, signingKeysIndexDDL); err != nil {
		return err
	}
	return nil
}

func (s *PostgresStore) Active(ctx context.Context) (*Record, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+signingKeyColumns+`
           FROM jwt_signing_keys
          WHERE status = 'active'
          ORDER BY not_after DESC LIMIT 1`)
	rec, err := scanSigningKeyRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *PostgresStore) Retiring(ctx context.Context, now time.Time) ([]Record, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+signingKeyColumns+`
           FROM jwt_signing_keys
          WHERE status = 'retiring' AND not_after > $1
          ORDER BY not_after DESC`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Record, 0)
	for rows.Next() {
		rec, err := scanSigningKeyRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *PostgresStore) All(ctx context.Context) ([]Record, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+signingKeyColumns+`
           FROM jwt_signing_keys
          ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Record, 0)
	for rows.Next() {
		rec, err := scanSigningKeyRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Insert(ctx context.Context, rec Record) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO jwt_signing_keys
            (kid, algorithm, public_key_pem, private_key_enc,
             created_at, not_before, not_after, status)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		rec.Kid, rec.Algorithm, rec.PublicKeyPEM, rec.PrivateKeyEnc,
		rec.CreatedAt, rec.NotBefore, rec.NotAfter, string(rec.Status))
	return err
}

// Rotate demotes the previously-active row to 'retiring' (with
// not_after = retireAt) and inserts `next` as the new 'active' row,
// inside a single transaction.
func (s *PostgresStore) Rotate(ctx context.Context, previousKid string, next Record, retireAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if previousKid != "" {
		if _, err := tx.Exec(ctx,
			`UPDATE jwt_signing_keys
                SET status = 'retiring', not_after = $2
              WHERE kid = $1 AND status = 'active'`,
			previousKid, retireAt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO jwt_signing_keys
            (kid, algorithm, public_key_pem, private_key_enc,
             created_at, not_before, not_after, status)
         VALUES ($1, $2, $3, $4, $5, $6, $7, 'active')`,
		next.Kid, next.Algorithm, next.PublicKeyPEM, next.PrivateKeyEnc,
		next.CreatedAt, next.NotBefore, next.NotAfter); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// MarkExpired flips every retiring row whose not_after has passed to
// 'retired'. Returns the number of rows updated.
func (s *PostgresStore) MarkExpired(ctx context.Context, now time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE jwt_signing_keys
            SET status = 'retired'
          WHERE status = 'retiring' AND not_after <= $1`, now)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// pgScanner is the minimal Scan interface shared by pgx.Row + pgx.Rows.
type pgScanner interface {
	Scan(dest ...any) error
}

func scanSigningKeyRow(s pgScanner) (Record, error) {
	var (
		rec       Record
		statusStr string
	)
	if err := s.Scan(
		&rec.Kid, &rec.Algorithm, &rec.PublicKeyPEM, &rec.PrivateKeyEnc,
		&rec.CreatedAt, &rec.NotBefore, &rec.NotAfter, &statusStr,
	); err != nil {
		return rec, err
	}
	rec.Status = Status(statusStr)
	return rec, nil
}

// Compile-time interface check.
var _ Store = (*PostgresStore)(nil)

// ─── In-memory impl ────────────────────────────────────────────────────

// InMemoryStore is a thread-safe Store useful for tests + dev loops.
type InMemoryStore struct {
	mu   sync.Mutex
	rows map[string]Record
}

// NewInMemoryStore returns a freshly-initialised store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{rows: map[string]Record{}}
}

func (s *InMemoryStore) EnsureSchema(_ context.Context) error { return nil }

func (s *InMemoryStore) Active(_ context.Context) (*Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *Record
	for _, r := range s.rows {
		r := r
		if r.Status != StatusActive {
			continue
		}
		if best == nil || r.NotAfter.After(best.NotAfter) {
			best = &r
		}
	}
	return best, nil
}

func (s *InMemoryStore) Retiring(_ context.Context, now time.Time) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0)
	for _, r := range s.rows {
		if r.Status != StatusRetiring {
			continue
		}
		if !r.NotAfter.After(now) {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].NotAfter.After(out[j].NotAfter) })
	return out, nil
}

func (s *InMemoryStore) All(_ context.Context) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *InMemoryStore) Insert(_ context.Context, rec Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[rec.Kid] = rec
	return nil
}

func (s *InMemoryStore) Rotate(_ context.Context, previousKid string, next Record, retireAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if previousKid != "" {
		if prev, ok := s.rows[previousKid]; ok && prev.Status == StatusActive {
			prev.Status = StatusRetiring
			prev.NotAfter = retireAt
			s.rows[previousKid] = prev
		}
	}
	s.rows[next.Kid] = next
	return nil
}

func (s *InMemoryStore) MarkExpired(_ context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for kid, r := range s.rows {
		if r.Status == StatusRetiring && !r.NotAfter.After(now) {
			r.Status = StatusRetired
			s.rows[kid] = r
			n++
		}
	}
	return n, nil
}

// Compile-time interface check.
var _ Store = (*InMemoryStore)(nil)
