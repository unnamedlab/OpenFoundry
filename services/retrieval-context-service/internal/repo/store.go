package repo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/models"
)

// Store is the persistence boundary the handlers consume.
//
// Production wires PgStore (pgxpool). Unit tests wire MemoryStore.
type Store interface {
	CreateJob(ctx context.Context, j models.Job) (models.Job, error)
	GetJob(ctx context.Context, id uuid.UUID) (models.Job, error)
	ListJobs(ctx context.Context, filter ListJobsFilter) ([]models.Job, *string, error)
	UpdateJob(ctx context.Context, id uuid.UUID, patch models.UpdateJobRequest) (models.Job, error)
	DeleteJob(ctx context.Context, id uuid.UUID) error

	AppendEvent(ctx context.Context, e models.StatusEvent) (models.StatusEvent, error)
	ListEvents(ctx context.Context, jobID uuid.UUID) ([]models.StatusEvent, error)

	RecordExtraction(ctx context.Context, e models.Extraction) (models.Extraction, error)
	ListExtractions(ctx context.Context, jobID uuid.UUID) ([]models.Extraction, error)
}

// ListJobsFilter is the inbound paging + status filter for ListJobs.
type ListJobsFilter struct {
	Status   models.JobStatus
	Pipeline string
	Limit    int32
	Cursor   *string
}

const jobCols = `id, source_uri, mime_type, pipeline, status, options, created_at, updated_at`
const eventCols = `id, job_id, status, message, created_at`
const extractionCols = `id, job_id, extraction_kind, payload, confidence, created_at`

// ---------------------------------------------------------------------------
// PgStore — production pgx-backed implementation.
// ---------------------------------------------------------------------------

// PgStore implements Store on top of pgxpool.
type PgStore struct{ Pool *pgxpool.Pool }

// NewPgStore returns a PgStore backed by pool.
func NewPgStore(pool *pgxpool.Pool) *PgStore { return &PgStore{Pool: pool} }

type rowScanner interface{ Scan(...any) error }

func scanJob(s rowScanner) (models.Job, error) {
	var j models.Job
	var mime *string
	var optsRaw []byte
	if err := s.Scan(&j.ID, &j.SourceURI, &mime, &j.Pipeline, &j.Status, &optsRaw, &j.CreatedAt, &j.UpdatedAt); err != nil {
		return models.Job{}, err
	}
	j.MimeType = mime
	if len(optsRaw) == 0 {
		j.Options = json.RawMessage("{}")
	} else {
		j.Options = optsRaw
	}
	return j, nil
}

func scanEvent(s rowScanner) (models.StatusEvent, error) {
	var e models.StatusEvent
	var msg *string
	if err := s.Scan(&e.ID, &e.JobID, &e.Status, &msg, &e.CreatedAt); err != nil {
		return models.StatusEvent{}, err
	}
	e.Message = msg
	return e, nil
}

func scanExtraction(s rowScanner) (models.Extraction, error) {
	var e models.Extraction
	var conf *float32
	var payloadRaw []byte
	if err := s.Scan(&e.ID, &e.JobID, &e.ExtractionKind, &payloadRaw, &conf, &e.CreatedAt); err != nil {
		return models.Extraction{}, err
	}
	e.Confidence = conf
	if len(payloadRaw) == 0 {
		e.Payload = json.RawMessage("{}")
	} else {
		e.Payload = payloadRaw
	}
	return e, nil
}

func (s *PgStore) CreateJob(ctx context.Context, j models.Job) (models.Job, error) {
	opts := j.Options
	if len(opts) == 0 {
		opts = json.RawMessage("{}")
	}
	row := s.Pool.QueryRow(ctx,
		`INSERT INTO document_intelligence_jobs
		     (id, source_uri, mime_type, pipeline, status, options)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+jobCols,
		j.ID, j.SourceURI, j.MimeType, j.Pipeline, string(j.Status), []byte(opts),
	)
	return scanJob(row)
}

func (s *PgStore) GetJob(ctx context.Context, id uuid.UUID) (models.Job, error) {
	row := s.Pool.QueryRow(ctx, `SELECT `+jobCols+` FROM document_intelligence_jobs WHERE id = $1`, id)
	j, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Job{}, domain.ErrNotFound
	}
	return j, err
}

func (s *PgStore) ListJobs(ctx context.Context, f ListJobsFilter) ([]models.Job, *string, error) {
	args := make([]any, 0, 4)
	conds := make([]string, 0, 3)
	if f.Status != "" {
		args = append(args, string(f.Status))
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	if strings.TrimSpace(f.Pipeline) != "" {
		args = append(args, f.Pipeline)
		conds = append(conds, fmt.Sprintf("pipeline = $%d", len(args)))
	}
	if f.Cursor != nil {
		ts, id, err := decodeCursor(*f.Cursor)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: cursor: %w", domain.ErrInvalidInput, err)
		}
		args = append(args, ts, id)
		conds = append(conds, fmt.Sprintf("(created_at, id) < ($%d, $%d)", len(args)-1, len(args)))
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit+1)
	q := "SELECT " + jobCols + " FROM document_intelligence_jobs"
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(args))

	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := make([]models.Job, 0, limit)
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	var next *string
	if int32(len(out)) > limit {
		last := out[limit-1]
		c := encodeCursor(last.CreatedAt, last.ID)
		next = &c
		out = out[:limit]
	}
	return out, next, nil
}

func (s *PgStore) UpdateJob(ctx context.Context, id uuid.UUID, patch models.UpdateJobRequest) (models.Job, error) {
	sets := make([]string, 0, 4)
	args := make([]any, 0, 5)
	if patch.Status != nil {
		args = append(args, string(*patch.Status))
		sets = append(sets, fmt.Sprintf("status = $%d", len(args)))
	}
	if patch.MimeType != nil {
		args = append(args, *patch.MimeType)
		sets = append(sets, fmt.Sprintf("mime_type = $%d", len(args)))
	}
	if len(patch.Options) > 0 {
		args = append(args, []byte(patch.Options))
		sets = append(sets, fmt.Sprintf("options = $%d", len(args)))
	}
	if len(sets) == 0 {
		return models.Job{}, fmt.Errorf("%w: nothing to update", domain.ErrInvalidInput)
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)
	q := `UPDATE document_intelligence_jobs SET ` + strings.Join(sets, ", ") +
		fmt.Sprintf(` WHERE id = $%d RETURNING `, len(args)) + jobCols
	row := s.Pool.QueryRow(ctx, q, args...)
	j, err := scanJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Job{}, domain.ErrNotFound
	}
	return j, err
}

func (s *PgStore) DeleteJob(ctx context.Context, id uuid.UUID) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM document_intelligence_jobs WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *PgStore) AppendEvent(ctx context.Context, e models.StatusEvent) (models.StatusEvent, error) {
	row := s.Pool.QueryRow(ctx,
		`INSERT INTO document_intelligence_status_events (id, job_id, status, message)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+eventCols,
		e.ID, e.JobID, string(e.Status), e.Message,
	)
	ev, err := scanEvent(row)
	if isForeignKeyViolation(err) {
		return models.StatusEvent{}, domain.ErrNotFound
	}
	return ev, err
}

func (s *PgStore) ListEvents(ctx context.Context, jobID uuid.UUID) ([]models.StatusEvent, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT `+eventCols+` FROM document_intelligence_status_events
		  WHERE job_id = $1 ORDER BY created_at ASC, id ASC`,
		jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.StatusEvent, 0)
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *PgStore) RecordExtraction(ctx context.Context, e models.Extraction) (models.Extraction, error) {
	payload := e.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	row := s.Pool.QueryRow(ctx,
		`INSERT INTO document_intelligence_extractions
		     (id, job_id, extraction_kind, payload, confidence)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+extractionCols,
		e.ID, e.JobID, e.ExtractionKind, []byte(payload), e.Confidence,
	)
	ex, err := scanExtraction(row)
	if isForeignKeyViolation(err) {
		return models.Extraction{}, domain.ErrNotFound
	}
	return ex, err
}

func (s *PgStore) ListExtractions(ctx context.Context, jobID uuid.UUID) ([]models.Extraction, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT `+extractionCols+` FROM document_intelligence_extractions
		  WHERE job_id = $1 ORDER BY created_at ASC, id ASC`,
		jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Extraction, 0)
	for rows.Next() {
		ex, err := scanExtraction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ex)
	}
	return out, rows.Err()
}

// isForeignKeyViolation reports whether err is a pgx FK violation (SQLSTATE 23503).
// Used so AppendEvent / RecordExtraction return ErrNotFound when the
// referenced job_id does not exist.
func isForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	return false
}

// encodeCursor packs (created_at, id) into a base64 token. The wire
// shape is `<rfc3339Nano>|<uuid>` so the cursor remains debuggable in
// logs without giving callers a stable representation to depend on.
func encodeCursor(t time.Time, id uuid.UUID) string {
	raw := t.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(c string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, errors.New("malformed cursor")
	}
	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	return ts, id, nil
}

// ---------------------------------------------------------------------------
// MemoryStore — in-process Store for unit tests + dev without Postgres.
// ---------------------------------------------------------------------------

// MemoryStore is a concurrency-safe Store backed by Go maps.
type MemoryStore struct {
	mu          sync.RWMutex
	jobs        map[uuid.UUID]models.Job
	events      map[uuid.UUID][]models.StatusEvent
	extractions map[uuid.UUID][]models.Extraction
}

// NewMemoryStore returns an empty MemoryStore ready for use.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		jobs:        map[uuid.UUID]models.Job{},
		events:      map[uuid.UUID][]models.StatusEvent{},
		extractions: map[uuid.UUID][]models.Extraction{},
	}
}

func (s *MemoryStore) CreateJob(_ context.Context, j models.Job) (models.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	j.CreatedAt = now
	j.UpdatedAt = now
	if len(j.Options) == 0 {
		j.Options = json.RawMessage("{}")
	}
	s.jobs[j.ID] = j
	return j, nil
}

func (s *MemoryStore) GetJob(_ context.Context, id uuid.UUID) (models.Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return models.Job{}, domain.ErrNotFound
	}
	return j, nil
}

func (s *MemoryStore) ListJobs(_ context.Context, f ListJobsFilter) ([]models.Job, *string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	all := make([]models.Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		if f.Status != "" && j.Status != f.Status {
			continue
		}
		if strings.TrimSpace(f.Pipeline) != "" && j.Pipeline != f.Pipeline {
			continue
		}
		all = append(all, j)
	}
	sort.SliceStable(all, func(i, k int) bool {
		if all[i].CreatedAt.Equal(all[k].CreatedAt) {
			return all[i].ID.String() > all[k].ID.String()
		}
		return all[i].CreatedAt.After(all[k].CreatedAt)
	})

	if f.Cursor != nil {
		ts, id, err := decodeCursor(*f.Cursor)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: cursor: %w", domain.ErrInvalidInput, err)
		}
		filtered := all[:0]
		for _, j := range all {
			if j.CreatedAt.Before(ts) || (j.CreatedAt.Equal(ts) && j.ID.String() < id.String()) {
				filtered = append(filtered, j)
			}
		}
		all = filtered
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	var next *string
	if int32(len(all)) > limit {
		last := all[limit-1]
		c := encodeCursor(last.CreatedAt, last.ID)
		next = &c
		all = all[:limit]
	}
	return all, next, nil
}

func (s *MemoryStore) UpdateJob(_ context.Context, id uuid.UUID, patch models.UpdateJobRequest) (models.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return models.Job{}, domain.ErrNotFound
	}
	if patch.Status != nil {
		j.Status = *patch.Status
	}
	if patch.MimeType != nil {
		j.MimeType = patch.MimeType
	}
	if len(patch.Options) > 0 {
		j.Options = patch.Options
	}
	j.UpdatedAt = time.Now().UTC()
	s.jobs[id] = j
	return j, nil
}

func (s *MemoryStore) DeleteJob(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[id]; !ok {
		return domain.ErrNotFound
	}
	delete(s.jobs, id)
	delete(s.events, id)
	delete(s.extractions, id)
	return nil
}

func (s *MemoryStore) AppendEvent(_ context.Context, e models.StatusEvent) (models.StatusEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[e.JobID]; !ok {
		return models.StatusEvent{}, domain.ErrNotFound
	}
	e.CreatedAt = time.Now().UTC()
	s.events[e.JobID] = append(s.events[e.JobID], e)
	return e, nil
}

func (s *MemoryStore) ListEvents(_ context.Context, jobID uuid.UUID) ([]models.StatusEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.jobs[jobID]; !ok {
		return nil, domain.ErrNotFound
	}
	src := s.events[jobID]
	out := make([]models.StatusEvent, len(src))
	copy(out, src)
	sort.SliceStable(out, func(i, k int) bool {
		if out[i].CreatedAt.Equal(out[k].CreatedAt) {
			return out[i].ID.String() < out[k].ID.String()
		}
		return out[i].CreatedAt.Before(out[k].CreatedAt)
	})
	return out, nil
}

func (s *MemoryStore) RecordExtraction(_ context.Context, e models.Extraction) (models.Extraction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[e.JobID]; !ok {
		return models.Extraction{}, domain.ErrNotFound
	}
	if len(e.Payload) == 0 {
		e.Payload = json.RawMessage("{}")
	}
	e.CreatedAt = time.Now().UTC()
	s.extractions[e.JobID] = append(s.extractions[e.JobID], e)
	return e, nil
}

func (s *MemoryStore) ListExtractions(_ context.Context, jobID uuid.UUID) ([]models.Extraction, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.jobs[jobID]; !ok {
		return nil, domain.ErrNotFound
	}
	src := s.extractions[jobID]
	out := make([]models.Extraction, len(src))
	copy(out, src)
	sort.SliceStable(out, func(i, k int) bool {
		if out[i].CreatedAt.Equal(out[k].CreatedAt) {
			return out[i].ID.String() < out[k].ID.String()
		}
		return out[i].CreatedAt.Before(out[k].CreatedAt)
	})
	return out, nil
}

var _ Store = (*PgStore)(nil)
var _ Store = (*MemoryStore)(nil)
