// Postgres-backed Repository implementation. Mirrors the in-memory
// store's semantics so handler tests can run against either backend.
// JSONB columns (labels, pipeline_io_config, container_image,
// runtime_config, payload, result) are marshalled with encoding/json on
// the way in and unmarshalled on the way out.
//
// Cursor pagination uses keyset on (created_at, id). The cursor token
// is the last-row UUID; the lookup resolves its (created_at, id) tuple
// inside the same statement so the page boundary stays stable when the
// caller paginates over an active dataset.
package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/domain/function"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/models"
)

// PgRepository is the pgx-backed Repository implementation.
type PgRepository struct {
	Pool *pgxpool.Pool
}

// NewPgRepository wraps a pgxpool in the Repository contract. Callers
// are responsible for closing the pool.
func NewPgRepository(pool *pgxpool.Pool) *PgRepository {
	return &PgRepository{Pool: pool}
}

const moduleColumns = `id, name, description, project_id, folder_id,
                       execution_mode, state, labels,
                       pipeline_io_config, container_image, runtime_config,
                       created_at, updated_at, created_by, updated_by,
                       archived_at, archived_by`

type rowScanner interface {
	Scan(dest ...any) error
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func mapNameConflict(err error) error {
	if isUniqueViolation(err) {
		return ErrNameConflict
	}
	return err
}

func marshalLabels(labels map[string]string) ([]byte, error) {
	if labels == nil {
		labels = map[string]string{}
	}
	return json.Marshal(labels)
}

func marshalNullable(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}

func scanModule(s rowScanner) (*models.ComputeModule, error) {
	var (
		m            models.ComputeModule
		folderID     *uuid.UUID
		labelsRaw    []byte
		pipelineRaw  []byte
		containerRaw []byte
		runtimeRaw   []byte
		archivedAt   *time.Time
		archivedBy   *uuid.UUID
	)
	if err := s.Scan(
		&m.ID, &m.Name, &m.Description, &m.ProjectID, &folderID,
		&m.ExecutionMode, &m.State, &labelsRaw,
		&pipelineRaw, &containerRaw, &runtimeRaw,
		&m.CreatedAt, &m.UpdatedAt, &m.CreatedBy, &m.UpdatedBy,
		&archivedAt, &archivedBy,
	); err != nil {
		return nil, err
	}
	m.FolderID = folderID
	m.ArchivedAt = archivedAt
	m.ArchivedBy = archivedBy

	if len(labelsRaw) > 0 && string(labelsRaw) != "null" {
		labels := map[string]string{}
		if err := json.Unmarshal(labelsRaw, &labels); err != nil {
			return nil, fmt.Errorf("decode labels: %w", err)
		}
		if len(labels) > 0 {
			m.Labels = labels
		}
	}
	if len(pipelineRaw) > 0 && string(pipelineRaw) != "null" {
		var pipeline models.PipelineIOConfig
		if err := json.Unmarshal(pipelineRaw, &pipeline); err != nil {
			return nil, fmt.Errorf("decode pipeline_io_config: %w", err)
		}
		m.PipelineIOConfig = &pipeline
	}
	if len(containerRaw) > 0 && string(containerRaw) != "null" {
		var img models.ContainerImage
		if err := json.Unmarshal(containerRaw, &img); err != nil {
			return nil, fmt.Errorf("decode container_image: %w", err)
		}
		m.ContainerImage = &img
	}
	if len(runtimeRaw) > 0 && string(runtimeRaw) != "null" {
		var rc models.RuntimeConfig
		if err := json.Unmarshal(runtimeRaw, &rc); err != nil {
			return nil, fmt.Errorf("decode runtime_config: %w", err)
		}
		m.RuntimeConfig = &rc
	}
	return &m, nil
}

// Create inserts a new module in lifecycle=active. The unique index
// `uq_compute_modules_active_name` enforces project+folder name
// uniqueness; the violation surfaces as ErrNameConflict.
func (r *PgRepository) Create(ctx context.Context, p models.CreateParams) (*models.ComputeModule, error) {
	labelsRaw, err := marshalLabels(p.Labels)
	if err != nil {
		return nil, err
	}
	id := uuid.New()
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO compute_modules
		    (id, name, description, project_id, folder_id, execution_mode, state, labels,
		     created_by, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6, 'active', $7::jsonb, $8, $8)
		 RETURNING `+moduleColumns,
		id, p.Name, p.Description, p.ProjectID, p.FolderID, string(p.ExecutionMode), labelsRaw, p.Actor)
	m, err := scanModule(row)
	if err != nil {
		return nil, mapNameConflict(err)
	}
	return m, nil
}

// Get returns the module by id (active or archived). pgx.ErrNoRows
// surfaces as ErrNotFound.
func (r *PgRepository) Get(ctx context.Context, id uuid.UUID) (*models.ComputeModule, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+moduleColumns+` FROM compute_modules WHERE id = $1`, id)
	m, err := scanModule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// List paginates by (created_at, id) keyset. Cursor decodes the
// previous page's last UUID.
func (r *PgRepository) List(ctx context.Context, filter ListFilter, page Page) (ListResult, error) {
	limit := page.Limit
	if limit == 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var (
		conds []string
		args  []any
	)
	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}

	if filter.State != nil {
		add("state = $%d", string(*filter.State))
	} else if !filter.IncludeArchived {
		conds = append(conds, "state = 'active'")
	}
	if filter.ProjectID != nil {
		add("project_id = $%d", *filter.ProjectID)
	}
	if filter.FolderID != nil {
		add("folder_id = $%d", *filter.FolderID)
	}
	if filter.ExecutionMode != nil {
		add("execution_mode = $%d", string(*filter.ExecutionMode))
	}

	var (
		cursorCreated time.Time
		cursorID      uuid.UUID
		hasCursor     bool
	)
	if page.Cursor != nil && *page.Cursor != "" {
		parsed, err := uuid.Parse(*page.Cursor)
		if err == nil {
			err = r.Pool.QueryRow(ctx,
				`SELECT created_at, id FROM compute_modules WHERE id = $1`, parsed,
			).Scan(&cursorCreated, &cursorID)
			if err == nil {
				hasCursor = true
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return ListResult{}, err
			}
		}
	}
	if hasCursor {
		args = append(args, cursorCreated, cursorID)
		conds = append(conds, fmt.Sprintf("(created_at, id) > ($%d, $%d)", len(args)-1, len(args)))
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, int(limit)+1)

	query := fmt.Sprintf(`SELECT %s FROM compute_modules %s ORDER BY created_at, id LIMIT $%d`,
		moduleColumns, where, len(args))
	rows, err := r.Pool.Query(ctx, query, args...)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()

	out := ListResult{Items: make([]*models.ComputeModule, 0, limit)}
	for rows.Next() {
		m, err := scanModule(rows)
		if err != nil {
			return ListResult{}, err
		}
		out.Items = append(out.Items, m)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	if len(out.Items) > int(limit) {
		out.Items = out.Items[:limit]
		last := out.Items[len(out.Items)-1].ID.String()
		out.NextCursor = &last
	}
	return out, nil
}

// UpdateMetadata applies an optional patch (name/description/labels).
// At least one field must be set; the caller validates that.
func (r *PgRepository) UpdateMetadata(ctx context.Context, id uuid.UUID, p models.UpdateMetadataParams) (*models.ComputeModule, error) {
	var labelsArg any
	if p.Labels != nil {
		raw, err := marshalLabels(*p.Labels)
		if err != nil {
			return nil, err
		}
		labelsArg = raw
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE compute_modules
		    SET name = COALESCE($2, name),
		        description = COALESCE($3, description),
		        labels = COALESCE($4::jsonb, labels),
		        updated_at = NOW(),
		        updated_by = $5
		  WHERE id = $1
		  RETURNING `+moduleColumns,
		id, p.Name, p.Description, labelsArg, p.Actor)
	m, err := scanModule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, mapNameConflict(err)
	}
	return m, nil
}

// Move rehomes a module under a new project/folder. The unique index
// catches name collisions in the destination as ErrNameConflict.
func (r *PgRepository) Move(ctx context.Context, id uuid.UUID, p models.MoveParams) (*models.ComputeModule, error) {
	row := r.Pool.QueryRow(ctx,
		`UPDATE compute_modules
		    SET project_id = $2,
		        folder_id = $3,
		        updated_at = NOW(),
		        updated_by = $4
		  WHERE id = $1
		  RETURNING `+moduleColumns,
		id, p.ProjectID, p.FolderID, p.Actor)
	m, err := scanModule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, mapNameConflict(err)
	}
	return m, nil
}

// Duplicate clones a module's metadata into a brand-new active record.
// The new id is minted server-side; execution mode and labels carry
// over.
func (r *PgRepository) Duplicate(ctx context.Context, id uuid.UUID, p models.DuplicateParams) (*models.ComputeModule, error) {
	newID := uuid.New()
	var projectArg, folderArg any
	if p.ProjectID != nil {
		projectArg = *p.ProjectID
	}
	if p.FolderID != nil {
		folderArg = *p.FolderID
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO compute_modules
		    (id, name, description, project_id, folder_id, execution_mode, state,
		     labels, created_by, updated_by)
		 SELECT $1,
		        $2,
		        description,
		        COALESCE($3::uuid, project_id),
		        COALESCE($4::uuid, folder_id),
		        execution_mode,
		        'active',
		        labels,
		        $5, $5
		   FROM compute_modules
		  WHERE id = $6
		 RETURNING `+moduleColumns,
		newID, p.NewName, projectArg, folderArg, p.Actor, id)
	m, err := scanModule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, mapNameConflict(err)
	}
	return m, nil
}

// Archive flips the module to lifecycle=archived. Re-archiving an
// already-archived row surfaces as ErrAlreadyArchived (idempotency
// boundary for audit).
func (r *PgRepository) Archive(ctx context.Context, id, actor uuid.UUID) (*models.ComputeModule, error) {
	row := r.Pool.QueryRow(ctx,
		`UPDATE compute_modules
		    SET state = 'archived',
		        archived_at = NOW(),
		        archived_by = $2,
		        updated_at = NOW(),
		        updated_by = $2
		  WHERE id = $1
		    AND state = 'active'
		  RETURNING `+moduleColumns,
		id, actor)
	m, err := scanModule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		exists, lookupErr := r.exists(ctx, id)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if !exists {
			return nil, ErrNotFound
		}
		return nil, ErrAlreadyArchived
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// Restore returns an archived module to active. Name uniqueness in the
// destination is re-checked by the unique index.
func (r *PgRepository) Restore(ctx context.Context, id, actor uuid.UUID) (*models.ComputeModule, error) {
	row := r.Pool.QueryRow(ctx,
		`UPDATE compute_modules
		    SET state = 'active',
		        archived_at = NULL,
		        archived_by = NULL,
		        updated_at = NOW(),
		        updated_by = $2
		  WHERE id = $1
		    AND state = 'archived'
		  RETURNING `+moduleColumns,
		id, actor)
	m, err := scanModule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		exists, lookupErr := r.exists(ctx, id)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if !exists {
			return nil, ErrNotFound
		}
		return nil, ErrNotArchived
	}
	if err != nil {
		return nil, mapNameConflict(err)
	}
	return m, nil
}

// Delete hard-removes the row. Callers should normally Archive first;
// hard delete is gated by permissions at the handler layer.
func (r *PgRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.Pool.Exec(ctx, `DELETE FROM compute_modules WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPipelineIOConfig persists a pipeline I/O config on a pipeline-mode
// module. Function-mode modules surface as ErrExecutionModeMismatch
// — the row is pre-checked so the response is a deterministic 409
// rather than a generic CHECK violation.
func (r *PgRepository) SetPipelineIOConfig(ctx context.Context, id uuid.UUID, cfg models.PipelineIOConfig, actor uuid.UUID) (*models.ComputeModule, error) {
	if err := r.requireExecutionMode(ctx, id, models.ExecutionModePipeline); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE compute_modules
		    SET pipeline_io_config = $2::jsonb,
		        updated_at = NOW(),
		        updated_by = $3
		  WHERE id = $1
		  RETURNING `+moduleColumns,
		id, raw, actor)
	m, err := scanModule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ClearPipelineIOConfig drops the pipeline I/O config (pipeline mode
// only).
func (r *PgRepository) ClearPipelineIOConfig(ctx context.Context, id, actor uuid.UUID) (*models.ComputeModule, error) {
	if err := r.requireExecutionMode(ctx, id, models.ExecutionModePipeline); err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE compute_modules
		    SET pipeline_io_config = NULL,
		        updated_at = NOW(),
		        updated_by = $2
		  WHERE id = $1
		  RETURNING `+moduleColumns,
		id, actor)
	m, err := scanModule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// SetContainerImage stores the image reference + findings on the row.
func (r *PgRepository) SetContainerImage(ctx context.Context, id uuid.UUID, img models.ContainerImage, actor uuid.UUID) (*models.ComputeModule, error) {
	raw, err := json.Marshal(img)
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE compute_modules
		    SET container_image = $2::jsonb,
		        updated_at = NOW(),
		        updated_by = $3
		  WHERE id = $1
		  RETURNING `+moduleColumns,
		id, raw, actor)
	m, err := scanModule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ClearContainerImage removes any image reference.
func (r *PgRepository) ClearContainerImage(ctx context.Context, id, actor uuid.UUID) (*models.ComputeModule, error) {
	row := r.Pool.QueryRow(ctx,
		`UPDATE compute_modules
		    SET container_image = NULL,
		        updated_at = NOW(),
		        updated_by = $2
		  WHERE id = $1
		  RETURNING `+moduleColumns,
		id, actor)
	m, err := scanModule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// SetRuntimeConfig persists the runtime config (already redacted by the
// caller).
func (r *PgRepository) SetRuntimeConfig(ctx context.Context, id uuid.UUID, cfg models.RuntimeConfig, actor uuid.UUID) (*models.ComputeModule, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE compute_modules
		    SET runtime_config = $2::jsonb,
		        updated_at = NOW(),
		        updated_by = $3
		  WHERE id = $1
		  RETURNING `+moduleColumns,
		id, raw, actor)
	m, err := scanModule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// ClearRuntimeConfig drops the runtime config.
func (r *PgRepository) ClearRuntimeConfig(ctx context.Context, id, actor uuid.UUID) (*models.ComputeModule, error) {
	row := r.Pool.QueryRow(ctx,
		`UPDATE compute_modules
		    SET runtime_config = NULL,
		        updated_at = NOW(),
		        updated_by = $2
		  WHERE id = $1
		  RETURNING `+moduleColumns,
		id, actor)
	m, err := scanModule(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (r *PgRepository) exists(ctx context.Context, id uuid.UUID) (bool, error) {
	var found bool
	err := r.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM compute_modules WHERE id = $1)`, id,
	).Scan(&found)
	if err != nil {
		return false, err
	}
	return found, nil
}

func (r *PgRepository) requireExecutionMode(ctx context.Context, id uuid.UUID, mode models.ExecutionMode) error {
	var existing string
	err := r.Pool.QueryRow(ctx,
		`SELECT execution_mode FROM compute_modules WHERE id = $1`, id,
	).Scan(&existing)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if models.ExecutionMode(existing) != mode {
		return ErrExecutionModeMismatch
	}
	return nil
}

// ── invocations ─────────────────────────────────────────────────────

const invocationColumns = `id, module_id, module_version, function_name, payload,
                           tenant_id, actor_id, scheduled_at, started_at, finished_at,
                           status, result, error_message, cost_units`

func scanInvocation(s rowScanner) (*function.FunctionInvocation, error) {
	var (
		inv        function.FunctionInvocation
		payload    []byte
		startedAt  *time.Time
		finishedAt *time.Time
		result     []byte
	)
	if err := s.Scan(
		&inv.ID, &inv.ModuleID, &inv.ModuleVersion, &inv.FunctionName, &payload,
		&inv.TenantID, &inv.ActorID, &inv.ScheduledAt, &startedAt, &finishedAt,
		&inv.Status, &result, &inv.ErrorMessage, &inv.CostUnits,
	); err != nil {
		return nil, err
	}
	if len(payload) > 0 {
		inv.Payload = append(json.RawMessage(nil), payload...)
	}
	if len(result) > 0 && string(result) != "null" {
		inv.Result = append(json.RawMessage(nil), result...)
	}
	inv.StartedAt = startedAt
	inv.FinishedAt = finishedAt
	return &inv, nil
}

// CreateInvocation persists a freshly queued FunctionInvocation. The
// repo mints the id when the caller leaves it zero and stamps
// ScheduledAt.
func (r *PgRepository) CreateInvocation(ctx context.Context, in function.FunctionInvocation) (*function.FunctionInvocation, error) {
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	if in.Status == "" {
		in.Status = function.StatusQueued
	}
	payload := []byte(in.Payload)
	if len(payload) == 0 {
		payload = []byte("null")
	}
	scheduled := in.ScheduledAt
	if scheduled.IsZero() {
		scheduled = time.Now().UTC()
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO function_invocations
		    (id, module_id, module_version, function_name, payload,
		     tenant_id, actor_id, scheduled_at, status)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9)
		 RETURNING `+invocationColumns,
		in.ID, in.ModuleID, in.ModuleVersion, in.FunctionName, payload,
		in.TenantID, in.ActorID, scheduled, string(in.Status))
	return scanInvocation(row)
}

// MarkInvocationRunning flips a queued row to running and stamps
// StartedAt.
func (r *PgRepository) MarkInvocationRunning(ctx context.Context, id uuid.UUID) (*function.FunctionInvocation, error) {
	row := r.Pool.QueryRow(ctx,
		`UPDATE function_invocations
		    SET status = 'running', started_at = NOW()
		  WHERE id = $1
		    AND status = 'queued'
		  RETURNING `+invocationColumns,
		id)
	inv, err := scanInvocation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		exists, lookupErr := r.invocationExists(ctx, id)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if !exists {
			return nil, function.ErrInvocationNotFound
		}
		return nil, function.ErrInvocationTerminal
	}
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// CompleteInvocation moves the row to a terminal status. The CHECK
// constraint requires finished_at IS NOT NULL for terminal status and
// started_at IS NOT NULL except when queued, so we COALESCE started_at
// to scheduled_at when the row never reached running.
func (r *PgRepository) CompleteInvocation(ctx context.Context, id uuid.UUID, update InvocationCompletion) (*function.FunctionInvocation, error) {
	if !update.Status.IsValid() || !update.Status.IsTerminal() {
		return nil, function.ErrInvocationTerminal
	}
	var resultArg any
	if len(update.Result) > 0 {
		resultArg = []byte(update.Result)
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE function_invocations
		    SET status = $2,
		        started_at = COALESCE(started_at, scheduled_at),
		        finished_at = NOW(),
		        result = $3::jsonb,
		        error_message = $4,
		        cost_units = $5
		  WHERE id = $1
		    AND status IN ('queued', 'running')
		  RETURNING `+invocationColumns,
		id, string(update.Status), resultArg, update.ErrorMessage, update.CostUnits)
	inv, err := scanInvocation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		// Terminal rows are no-ops in the memory backend; here we
		// re-read so callers see the stored result.
		existing, lookupErr := r.GetInvocation(ctx, id)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if existing.Status.IsTerminal() {
			return existing, nil
		}
		return nil, function.ErrInvocationTerminal
	}
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// CancelInvocation flips a non-terminal row to cancelled. started_at
// is COALESCEd to NOW() so the CHECK on cancelled-from-queued holds.
func (r *PgRepository) CancelInvocation(ctx context.Context, id, _ uuid.UUID) (*function.FunctionInvocation, error) {
	row := r.Pool.QueryRow(ctx,
		`UPDATE function_invocations
		    SET status = 'cancelled',
		        started_at = COALESCE(started_at, NOW()),
		        finished_at = NOW()
		  WHERE id = $1
		    AND status IN ('queued', 'running')
		  RETURNING `+invocationColumns,
		id)
	inv, err := scanInvocation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		exists, lookupErr := r.invocationExists(ctx, id)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if !exists {
			return nil, function.ErrInvocationNotFound
		}
		return nil, function.ErrInvocationTerminal
	}
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// GetInvocation returns the row by id; pgx.ErrNoRows surfaces as
// ErrInvocationNotFound.
func (r *PgRepository) GetInvocation(ctx context.Context, id uuid.UUID) (*function.FunctionInvocation, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+invocationColumns+` FROM function_invocations WHERE id = $1`, id)
	inv, err := scanInvocation(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, function.ErrInvocationNotFound
	}
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// ListInvocations paginates by (scheduled_at, id) keyset.
func (r *PgRepository) ListInvocations(ctx context.Context, filter InvocationFilter, page Page) (InvocationListResult, error) {
	limit := page.Limit
	if limit == 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var (
		conds []string
		args  []any
	)
	add := func(cond string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}
	if filter.ModuleID != nil {
		add("module_id = $%d", *filter.ModuleID)
	}
	if filter.TenantID != nil {
		add("tenant_id = $%d", *filter.TenantID)
	}
	if filter.Status != nil {
		add("status = $%d", string(*filter.Status))
	}

	var (
		cursorScheduled time.Time
		cursorID        uuid.UUID
		hasCursor       bool
	)
	if page.Cursor != nil && *page.Cursor != "" {
		parsed, err := uuid.Parse(*page.Cursor)
		if err == nil {
			err = r.Pool.QueryRow(ctx,
				`SELECT scheduled_at, id FROM function_invocations WHERE id = $1`, parsed,
			).Scan(&cursorScheduled, &cursorID)
			if err == nil {
				hasCursor = true
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return InvocationListResult{}, err
			}
		}
	}
	if hasCursor {
		args = append(args, cursorScheduled, cursorID)
		conds = append(conds, fmt.Sprintf("(scheduled_at, id) > ($%d, $%d)", len(args)-1, len(args)))
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, int(limit)+1)
	query := fmt.Sprintf(`SELECT %s FROM function_invocations %s ORDER BY scheduled_at, id LIMIT $%d`,
		invocationColumns, where, len(args))
	rows, err := r.Pool.Query(ctx, query, args...)
	if err != nil {
		return InvocationListResult{}, err
	}
	defer rows.Close()

	out := InvocationListResult{Items: make([]*function.FunctionInvocation, 0, limit)}
	for rows.Next() {
		inv, err := scanInvocation(rows)
		if err != nil {
			return InvocationListResult{}, err
		}
		out.Items = append(out.Items, inv)
	}
	if err := rows.Err(); err != nil {
		return InvocationListResult{}, err
	}
	if len(out.Items) > int(limit) {
		out.Items = out.Items[:limit]
		last := out.Items[len(out.Items)-1].ID.String()
		out.NextCursor = &last
	}
	return out, nil
}

func (r *PgRepository) invocationExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var found bool
	err := r.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM function_invocations WHERE id = $1)`, id,
	).Scan(&found)
	if err != nil {
		return false, err
	}
	return found, nil
}

// Compile-time check that *PgRepository satisfies Repository.
var _ Repository = (*PgRepository)(nil)
