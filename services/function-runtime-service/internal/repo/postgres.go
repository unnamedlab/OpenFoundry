package repo

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every embedded `migrations/*.sql` file in lex order.
// Idempotent (CREATE TABLE IF NOT EXISTS).
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

// PostgresStore is the pgx-backed Store implementation.
type PostgresStore struct{ Pool *pgxpool.Pool }

// NewPostgresStore wraps an existing pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{Pool: pool} }

func (s *PostgresStore) CreateFunction(ctx context.Context, fn *models.FunctionDefinition) error {
	if fn.ID == uuid.Nil {
		fn.ID = ids.New()
	}
	sig, err := json.Marshal(fn.Signature)
	if err != nil {
		return fmt.Errorf("marshal signature: %w", err)
	}
	row := s.Pool.QueryRow(ctx, `
		INSERT INTO function_definitions
		    (id, tenant_id, namespace, name, runtime, signature, status, latest_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0)
		RETURNING created_at, updated_at, latest_version
	`,
		fn.ID, fn.TenantID, fn.Namespace, fn.Name, string(fn.Runtime), sig, string(fn.Status),
	)
	if err := row.Scan(&fn.CreatedAt, &fn.UpdatedAt, &fn.LatestVersion); err != nil {
		if isUniqueViolation(err) {
			return domain.ErrAlreadyExists
		}
		return err
	}
	return nil
}

func (s *PostgresStore) GetFunction(ctx context.Context, tenantID, id uuid.UUID) (*models.FunctionDefinition, error) {
	row := s.Pool.QueryRow(ctx, `
		SELECT id, tenant_id, namespace, name, runtime, signature, status,
		       active_version, latest_version, created_at, updated_at, activated_at
		  FROM function_definitions
		 WHERE id = $1 AND ($2::uuid = '00000000-0000-0000-0000-000000000000' OR tenant_id = $2)
	`, id, tenantID)
	return scanFunction(row)
}

func (s *PostgresStore) ListFunctions(ctx context.Context, f ListFunctionsFilter) ([]models.FunctionDefinition, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id, tenant_id, namespace, name, runtime, signature, status,
		       active_version, latest_version, created_at, updated_at, activated_at
		  FROM function_definitions
		 WHERE ($1::uuid = '00000000-0000-0000-0000-000000000000' OR tenant_id = $1)
		   AND ($2 = '' OR namespace = $2)
		   AND ($3 = '' OR status = $3)
		   AND ($4 = '' OR runtime = $4)
		 ORDER BY created_at DESC
		 LIMIT $5
	`, f.TenantID, f.Namespace, string(f.Status), string(f.Runtime), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.FunctionDefinition, 0)
	for rows.Next() {
		fn, err := scanFunctionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *fn)
	}
	return out, rows.Err()
}

func (s *PostgresStore) UpdateFunctionStatus(ctx context.Context, tenantID, id uuid.UUID, status models.Status, activeVersion *int) (*models.FunctionDefinition, error) {
	row := s.Pool.QueryRow(ctx, `
		UPDATE function_definitions
		   SET status = $3,
		       active_version = COALESCE($4, active_version),
		       activated_at = CASE WHEN $4 IS NOT NULL THEN now() ELSE activated_at END,
		       updated_at = now()
		 WHERE id = $1
		   AND ($2::uuid = '00000000-0000-0000-0000-000000000000' OR tenant_id = $2)
		RETURNING id, tenant_id, namespace, name, runtime, signature, status,
		          active_version, latest_version, created_at, updated_at, activated_at
	`, id, tenantID, string(status), activeVersion)
	return scanFunction(row)
}

func (s *PostgresStore) AppendVersion(ctx context.Context, tenantID, fnID uuid.UUID, sourceURI, entryPoint string) (*models.FunctionVersion, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var next int
	if err := tx.QueryRow(ctx, `
		UPDATE function_definitions
		   SET latest_version = latest_version + 1,
		       updated_at = now()
		 WHERE id = $1
		   AND ($2::uuid = '00000000-0000-0000-0000-000000000000' OR tenant_id = $2)
		RETURNING latest_version
	`, fnID, tenantID).Scan(&next); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	v := &models.FunctionVersion{
		ID:         ids.New(),
		FunctionID: fnID,
		Version:    next,
		SourceURI:  sourceURI,
		EntryPoint: entryPoint,
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO function_versions (id, function_id, version, source_uri, entry_point)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at
	`, v.ID, v.FunctionID, v.Version, v.SourceURI, v.EntryPoint).Scan(&v.CreatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return v, nil
}

func (s *PostgresStore) GetVersion(ctx context.Context, fnID uuid.UUID, version int) (*models.FunctionVersion, error) {
	row := s.Pool.QueryRow(ctx, `
		SELECT id, function_id, version, source_uri, entry_point, created_at
		  FROM function_versions WHERE function_id = $1 AND version = $2
	`, fnID, version)
	v := &models.FunctionVersion{}
	if err := row.Scan(&v.ID, &v.FunctionID, &v.Version, &v.SourceURI, &v.EntryPoint, &v.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrVersionNotFound
		}
		return nil, err
	}
	return v, nil
}

func (s *PostgresStore) ListVersions(ctx context.Context, fnID uuid.UUID) ([]models.FunctionVersion, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, function_id, version, source_uri, entry_point, created_at
		  FROM function_versions
		 WHERE function_id = $1
		 ORDER BY version DESC
	`, fnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.FunctionVersion, 0)
	for rows.Next() {
		v := models.FunctionVersion{}
		if err := rows.Scan(&v.ID, &v.FunctionID, &v.Version, &v.SourceURI, &v.EntryPoint, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *PostgresStore) CreateRun(ctx context.Context, run *models.FunctionRun) error {
	if run.ID == uuid.Nil {
		run.ID = ids.New()
	}
	if len(run.Input) == 0 {
		run.Input = json.RawMessage(`null`)
	}
	row := s.Pool.QueryRow(ctx, `
		INSERT INTO function_runs
		    (id, function_id, function_version, tenant_id, actor_id, status, input)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING started_at
	`, run.ID, run.FunctionID, run.FunctionVersion, run.TenantID, run.ActorID,
		string(run.Status), []byte(run.Input))
	return row.Scan(&run.StartedAt)
}

func (s *PostgresStore) GetRun(ctx context.Context, id uuid.UUID) (*models.FunctionRun, error) {
	row := s.Pool.QueryRow(ctx, `
		SELECT id, function_id, function_version, tenant_id, actor_id, status,
		       input, output, error, started_at, finished_at, duration_ms
		  FROM function_runs WHERE id = $1
	`, id)
	return scanRun(row)
}

func (s *PostgresStore) ListRuns(ctx context.Context, f ListRunsFilter) ([]models.FunctionRun, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT id, function_id, function_version, tenant_id, actor_id, status,
		       input, output, error, started_at, finished_at, duration_ms
		  FROM function_runs
		 WHERE ($1::uuid = '00000000-0000-0000-0000-000000000000' OR tenant_id = $1)
		   AND ($2::uuid = '00000000-0000-0000-0000-000000000000' OR function_id = $2)
		   AND ($3 = '' OR status = $3)
		 ORDER BY started_at DESC
		 LIMIT $4
	`, f.TenantID, f.FunctionID, string(f.Status), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.FunctionRun, 0)
	for rows.Next() {
		r, err := scanRunRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (s *PostgresStore) FinishRun(ctx context.Context, id uuid.UUID, upd RunUpdate) (*models.FunctionRun, error) {
	output := []byte(nil)
	if len(upd.Output) > 0 {
		output = upd.Output
	}
	row := s.Pool.QueryRow(ctx, `
		UPDATE function_runs
		   SET status = $2,
		       output = $3,
		       error = $4,
		       duration_ms = $5,
		       finished_at = now()
		 WHERE id = $1
		RETURNING id, function_id, function_version, tenant_id, actor_id, status,
		          input, output, error, started_at, finished_at, duration_ms
	`, id, string(upd.Status), output, upd.Error, upd.DurationMs)
	return scanRun(row)
}

// ─── scan helpers ─────────────────────────────────────────────────────

// scannable is the surface both pgx.Row and pgx.Rows satisfy for Scan.
type scannable interface {
	Scan(dest ...any) error
}

func scanFunction(row pgx.Row) (*models.FunctionDefinition, error) {
	fn, err := scanFunctionRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return fn, nil
}

func scanFunctionRow(row scannable) (*models.FunctionDefinition, error) {
	var (
		fn  models.FunctionDefinition
		sig []byte
		rt  string
		st  string
	)
	if err := row.Scan(
		&fn.ID, &fn.TenantID, &fn.Namespace, &fn.Name, &rt, &sig, &st,
		&fn.ActiveVersion, &fn.LatestVersion, &fn.CreatedAt, &fn.UpdatedAt, &fn.ActivatedAt,
	); err != nil {
		return nil, err
	}
	fn.Runtime = models.Runtime(rt)
	fn.Status = models.Status(st)
	if len(sig) > 0 {
		if err := json.Unmarshal(sig, &fn.Signature); err != nil {
			return nil, fmt.Errorf("unmarshal signature: %w", err)
		}
	}
	return &fn, nil
}

func scanRun(row pgx.Row) (*models.FunctionRun, error) {
	r, err := scanRunRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return r, nil
}

func scanRunRow(row scannable) (*models.FunctionRun, error) {
	var (
		r      models.FunctionRun
		input  []byte
		output []byte
		status string
	)
	if err := row.Scan(
		&r.ID, &r.FunctionID, &r.FunctionVersion, &r.TenantID, &r.ActorID, &status,
		&input, &output, &r.Error, &r.StartedAt, &r.FinishedAt, &r.DurationMs,
	); err != nil {
		return nil, err
	}
	r.Status = models.RunStatus(status)
	if len(input) > 0 {
		r.Input = append(json.RawMessage(nil), input...)
	}
	if len(output) > 0 {
		r.Output = append(json.RawMessage(nil), output...)
	}
	return &r, nil
}

// isUniqueViolation lifts a Postgres unique-violation (SQLSTATE 23505)
// out of a wrapped error. We avoid importing pgconn directly so the
// store stays buildable without a live database in tests.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	type sqlState interface{ SQLState() string }
	var st sqlState
	if errors.As(err, &st) {
		return st.SQLState() == "23505"
	}
	return false
}
