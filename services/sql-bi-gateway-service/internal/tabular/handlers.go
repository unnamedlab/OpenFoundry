package tabular

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

// Handlers hosts the tabular-analysis HTTP handlers.
type Handlers struct {
	Pool *pgxpool.Pool
	Log  *slog.Logger
}

// New builds a Handlers value backed by a pgx pool.
func New(pool *pgxpool.Pool, log *slog.Logger) *Handlers {
	return &Handlers{Pool: pool, Log: log}
}

// Mount wires every tabular route onto r.
func (h *Handlers) Mount(r chi.Router) {
	r.Get("/jobs", h.ListJobs)
	r.Post("/jobs", h.SubmitJob)
	r.Get("/jobs/{id}", h.GetJob)
	r.Get("/jobs/{id}/results", h.ListResults)
	r.Post("/jobs/{id}/results", h.PublishResult)
}

func (h *Handlers) errOut(label string, err error) {
	h.Log.Error("tabular "+label+" failed", slog.String("error", err.Error()))
}

// ListJobs returns the 200 most recent tabular-analysis jobs.
// GET /api/v1/tabular/jobs
func (h *Handlers) ListJobs(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, dataset_id, analysis_kind, status, options, created_at, updated_at
         FROM tabular_analysis_jobs ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		h.errOut("list_jobs", err)
		writePlain(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	out, err := scanJobs(rows)
	if err != nil {
		h.errOut("list_jobs scan", err)
		writePlain(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// SubmitJob enqueues a new tabular-analysis job.
// POST /api/v1/tabular/jobs
func (h *Handlers) SubmitJob(w http.ResponseWriter, r *http.Request) {
	var body SubmitJobRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writePlain(w, http.StatusBadRequest, err.Error())
		return
	}
	id := uuid.New()
	options := body.Options
	if len(options) == 0 {
		options = json.RawMessage(`{}`)
	}
	row := h.Pool.QueryRow(r.Context(),
		`INSERT INTO tabular_analysis_jobs (id, dataset_id, analysis_kind, status, options)
         VALUES ($1, $2, $3, 'queued', $4)
         RETURNING id, dataset_id, analysis_kind, status, options, created_at, updated_at`,
		id, body.DatasetID, body.AnalysisKind, []byte(options))
	job, err := scanJobRow(row)
	if err != nil {
		writePlain(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

// GetJob fetches a tabular-analysis job by id.
// GET /api/v1/tabular/jobs/:id
func (h *Handlers) GetJob(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writePlain(w, http.StatusBadRequest, "invalid id")
		return
	}
	row := h.Pool.QueryRow(r.Context(),
		`SELECT id, dataset_id, analysis_kind, status, options, created_at, updated_at
         FROM tabular_analysis_jobs WHERE id = $1`, id)
	job, err := scanJobRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writePlain(w, http.StatusNotFound, "job not found")
			return
		}
		writePlain(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// ListResults returns every result published for a given job.
// GET /api/v1/tabular/jobs/:id/results
func (h *Handlers) ListResults(w http.ResponseWriter, r *http.Request) {
	jobID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writePlain(w, http.StatusBadRequest, "invalid id")
		return
	}
	rows, err := h.Pool.Query(r.Context(),
		`SELECT id, job_id, result_kind, payload, created_at
         FROM tabular_analysis_results
         WHERE job_id = $1
         ORDER BY created_at DESC`, jobID)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	out, err := scanResults(rows)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// PublishResult records a new result for the given job.
// POST /api/v1/tabular/jobs/:id/results
func (h *Handlers) PublishResult(w http.ResponseWriter, r *http.Request) {
	jobID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writePlain(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body PublishResultRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writePlain(w, http.StatusBadRequest, err.Error())
		return
	}
	id := uuid.New()
	row := h.Pool.QueryRow(r.Context(),
		`INSERT INTO tabular_analysis_results (id, job_id, result_kind, payload)
         VALUES ($1, $2, $3, $4)
         RETURNING id, job_id, result_kind, payload, created_at`,
		id, jobID, body.ResultKind, []byte(body.Payload))
	res, err := scanResultRow(row)
	if err != nil {
		writePlain(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

// --- scanning helpers --------------------------------------------------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJobs(rows pgx.Rows) ([]AnalysisJob, error) {
	out := make([]AnalysisJob, 0)
	for rows.Next() {
		j, err := scanJobRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func scanJobRow(s rowScanner) (AnalysisJob, error) {
	var j AnalysisJob
	var options []byte
	if err := s.Scan(&j.ID, &j.DatasetID, &j.AnalysisKind, &j.Status,
		&options, &j.CreatedAt, &j.UpdatedAt); err != nil {
		return AnalysisJob{}, err
	}
	if len(options) > 0 {
		j.Options = json.RawMessage(options)
	}
	return j, nil
}

func scanResults(rows pgx.Rows) ([]AnalysisResult, error) {
	out := make([]AnalysisResult, 0)
	for rows.Next() {
		r, err := scanResultRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanResultRow(s rowScanner) (AnalysisResult, error) {
	var r AnalysisResult
	var payload []byte
	if err := s.Scan(&r.ID, &r.JobID, &r.ResultKind, &payload, &r.CreatedAt); err != nil {
		return AnalysisResult{}, err
	}
	if len(payload) > 0 {
		r.Payload = json.RawMessage(payload)
	}
	return r, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
