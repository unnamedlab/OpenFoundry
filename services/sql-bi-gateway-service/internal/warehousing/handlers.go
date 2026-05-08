package warehousing

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Handlers wraps the warehousing handlers with their database
// dependency. Mirrors the `AppState` shape used by the Rust axum
// handlers (the pool is the only piece of state).
type Handlers struct {
	Pool *pgxpool.Pool
	Log  *slog.Logger
}

// New builds a Handlers value backed by a pgx pool.
func New(pool *pgxpool.Pool, log *slog.Logger) *Handlers {
	return &Handlers{Pool: pool, Log: log}
}

// Mount wires every warehousing route onto r. Mirrors the routes
// declared in the Rust `crate::http::build_router`.
func (h *Handlers) Mount(r chi.Router) {
	r.Get("/jobs", h.ListJobs)
	r.Post("/jobs", h.SubmitJob)
	r.Get("/jobs/{id}", h.GetJob)
	r.Post("/jobs/{id}/cancel", h.CancelJob)
	r.Get("/transformations", h.ListTransformations)
	r.Post("/transformations", h.RegisterTransformation)
	r.Get("/transformations/{id}", h.GetTransformation)
	r.Get("/artifacts", h.ListArtifacts)
	r.Get("/artifacts/{id}", h.GetArtifact)
}

func (h *Handlers) dbError(label string, err error) {
	h.Log.Error("warehousing "+label+" failed", slog.String("error", err.Error()))
}

// ListJobs returns the 200 most recent warehousing jobs.
// GET /api/v1/warehouse/jobs
func (h *Handlers) ListJobs(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(), `
        SELECT id, slug, sql_text, status, source_datasets, target_dataset_id, target_storage_id,
               submitted_by, error_message, started_at, finished_at, created_at, updated_at
        FROM warehouse_jobs
        ORDER BY created_at DESC
        LIMIT 200`)
	if err != nil {
		h.dbError("list_jobs", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	jobs, err := scanJobs(rows)
	if err != nil {
		h.dbError("list_jobs scan", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": jobs})
}

// SubmitJob enqueues a new warehousing job.
// POST /api/v1/warehouse/jobs
func (h *Handlers) SubmitJob(w http.ResponseWriter, r *http.Request) {
	var body SubmitWarehouseJobRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	id := uuid.New()
	sources, err := json.Marshal(body.SourceDatasets)
	if err != nil {
		h.Log.Error("serialize source datasets failed", slog.String("error", err.Error()))
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	row := h.Pool.QueryRow(r.Context(), `
        INSERT INTO warehouse_jobs (id, slug, sql_text, status, source_datasets,
                                    target_dataset_id, target_storage_id)
        VALUES ($1, $2, $3, 'queued', $4::jsonb, $5, $6)
        RETURNING id, slug, sql_text, status, source_datasets, target_dataset_id, target_storage_id,
                  submitted_by, error_message, started_at, finished_at, created_at, updated_at`,
		id, body.Slug, body.SQLText, sources, body.TargetDatasetID, body.TargetStorageID)
	job, err := scanJobRow(row)
	if err != nil {
		h.dbError("submit_job", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

// GetJob fetches a warehousing job by id.
// GET /api/v1/warehouse/jobs/:id
func (h *Handlers) GetJob(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	row := h.Pool.QueryRow(r.Context(), `
        SELECT id, slug, sql_text, status, source_datasets, target_dataset_id, target_storage_id,
               submitted_by, error_message, started_at, finished_at, created_at, updated_at
        FROM warehouse_jobs WHERE id = $1`, id)
	job, err := scanJobRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		h.dbError("get_job", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// CancelJob marks a queued/running job as cancelled.
// POST /api/v1/warehouse/jobs/:id/cancel
func (h *Handlers) CancelJob(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	row := h.Pool.QueryRow(r.Context(), `
        UPDATE warehouse_jobs
        SET status = 'cancelled', finished_at = NOW(), updated_at = NOW()
        WHERE id = $1 AND status IN ('queued', 'running')
        RETURNING id, slug, sql_text, status, source_datasets, target_dataset_id, target_storage_id,
                  submitted_by, error_message, started_at, finished_at, created_at, updated_at`, id)
	job, err := scanJobRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusConflict)
			return
		}
		h.dbError("cancel_job", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// ListTransformations returns every reusable transformation, ordered by slug.
// GET /api/v1/warehouse/transformations
func (h *Handlers) ListTransformations(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(), `
        SELECT id, slug, description, sql_template, bindings, status, created_at, updated_at
        FROM warehouse_transformations
        ORDER BY slug`)
	if err != nil {
		h.dbError("list_transformations", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out, err := scanTransformations(rows)
	if err != nil {
		h.dbError("list_transformations scan", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

// RegisterTransformation upserts a transformation by slug.
// POST /api/v1/warehouse/transformations
func (h *Handlers) RegisterTransformation(w http.ResponseWriter, r *http.Request) {
	var body RegisterTransformationRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	id := uuid.New()
	bindings := body.Bindings
	if len(bindings) == 0 || string(bindings) == "null" {
		bindings = json.RawMessage(`{}`)
	}
	row := h.Pool.QueryRow(r.Context(), `
        INSERT INTO warehouse_transformations (id, slug, description, sql_template, bindings, status)
        VALUES ($1, $2, $3, $4, $5::jsonb, 'draft')
        ON CONFLICT (slug) DO UPDATE
        SET description = EXCLUDED.description,
            sql_template = EXCLUDED.sql_template,
            bindings = EXCLUDED.bindings,
            updated_at = NOW()
        RETURNING id, slug, description, sql_template, bindings, status, created_at, updated_at`,
		id, body.Slug, body.Description, body.SQLTemplate, []byte(bindings))
	t, err := scanTransformationRow(row)
	if err != nil {
		h.dbError("register_transformation", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// GetTransformation fetches a transformation by id.
// GET /api/v1/warehouse/transformations/:id
func (h *Handlers) GetTransformation(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	row := h.Pool.QueryRow(r.Context(), `
        SELECT id, slug, description, sql_template, bindings, status, created_at, updated_at
        FROM warehouse_transformations WHERE id = $1`, id)
	t, err := scanTransformationRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		h.dbError("get_transformation", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

// ListArtifacts returns the 200 most recent storage artifacts.
// GET /api/v1/warehouse/artifacts
func (h *Handlers) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(), `
        SELECT id, job_id, slug, artifact_kind, storage_uri, byte_size, row_count, status,
               expires_at, created_at, updated_at
        FROM warehouse_storage_artifacts
        ORDER BY created_at DESC
        LIMIT 200`)
	if err != nil {
		h.dbError("list_storage_artifacts", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	out, err := scanArtifacts(rows)
	if err != nil {
		h.dbError("list_storage_artifacts scan", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

// GetArtifact fetches a storage artifact by id.
// GET /api/v1/warehouse/artifacts/:id
func (h *Handlers) GetArtifact(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	row := h.Pool.QueryRow(r.Context(), `
        SELECT id, job_id, slug, artifact_kind, storage_uri, byte_size, row_count, status,
               expires_at, created_at, updated_at
        FROM warehouse_storage_artifacts WHERE id = $1`, id)
	a, err := scanArtifactRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		h.dbError("get_storage_artifact", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// --- scanning helpers --------------------------------------------------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJobs(rows pgx.Rows) ([]WarehouseJob, error) {
	out := make([]WarehouseJob, 0)
	for rows.Next() {
		j, err := scanJobRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func scanJobRow(s rowScanner) (WarehouseJob, error) {
	var j WarehouseJob
	var sources []byte
	if err := s.Scan(
		&j.ID, &j.Slug, &j.SQLText, &j.Status, &sources,
		&j.TargetDatasetID, &j.TargetStorageID,
		&j.SubmittedBy, &j.ErrorMessage, &j.StartedAt, &j.FinishedAt,
		&j.CreatedAt, &j.UpdatedAt,
	); err != nil {
		return WarehouseJob{}, err
	}
	if len(sources) > 0 {
		j.SourceDatasets = json.RawMessage(sources)
	}
	return j, nil
}

func scanTransformations(rows pgx.Rows) ([]WarehouseTransformation, error) {
	out := make([]WarehouseTransformation, 0)
	for rows.Next() {
		t, err := scanTransformationRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func scanTransformationRow(s rowScanner) (WarehouseTransformation, error) {
	var t WarehouseTransformation
	var bindings []byte
	if err := s.Scan(
		&t.ID, &t.Slug, &t.Description, &t.SQLTemplate, &bindings, &t.Status,
		&t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return WarehouseTransformation{}, err
	}
	if len(bindings) > 0 {
		t.Bindings = json.RawMessage(bindings)
	}
	return t, nil
}

func scanArtifacts(rows pgx.Rows) ([]WarehouseStorageArtifact, error) {
	out := make([]WarehouseStorageArtifact, 0)
	for rows.Next() {
		a, err := scanArtifactRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanArtifactRow(s rowScanner) (WarehouseStorageArtifact, error) {
	var a WarehouseStorageArtifact
	if err := s.Scan(
		&a.ID, &a.JobID, &a.Slug, &a.ArtifactKind, &a.StorageURI,
		&a.ByteSize, &a.RowCount, &a.Status, &a.ExpiresAt,
		&a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return WarehouseStorageArtifact{}, err
	}
	return a, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

