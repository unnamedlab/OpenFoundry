// Source CRUD + run lifecycle helpers for funnel repository.
//
// Mirrors the remaining 19 public symbols of
// `libs/ontology-kernel/src/domain/funnel_repository.rs` that the
// Phase 1 port left out: source create/update/delete, dataset and
// pipeline existence checks, health metrics aggregator, run lifecycle
// (create_run + complete_run + mark_source_ran + fail_run), and the
// per-source / per-run readers.
//
// The Phase 1 file (`funnel_repository.go`) carries `ListRunsForTenant`
// + the event payload / accumulator / decoder. We re-use those here and
// only add the surface the funnel handler reaches into.
package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// ── Inputs (mirror the Rust pub struct surface) ─────────────────────

// ListSourcesParams mirrors `pub struct ListSourcesParams<'a>`.
type ListSourcesParams struct {
	ObjectTypeID *uuid.UUID
	StatusFilter string
	IsAdmin      bool
	ActorID      uuid.UUID
	Offset       int64
	Limit        int64
}

// HealthSourcesParams mirrors `pub struct HealthSourcesParams`.
type HealthSourcesParams struct {
	ObjectTypeID *uuid.UUID
	IsAdmin      bool
	ActorID      uuid.UUID
}

// CreateSourceInput mirrors `pub struct CreateSourceInput`.
type CreateSourceInput struct {
	ID               uuid.UUID
	Name             string
	Description      string
	ObjectTypeID     uuid.UUID
	DatasetID        uuid.UUID
	PipelineID       *uuid.UUID
	DatasetBranch    *string
	DatasetVersion   *int32
	PreviewLimit     int32
	DefaultMarking   string
	Status           string
	PropertyMappings json.RawMessage
	TriggerContext   json.RawMessage
	OwnerID          uuid.UUID
}

// UpdateSourceInput mirrors `pub struct UpdateSourceInput`. Optional
// fields (`name`, `description`) keep `*string` semantics; the rest
// are required because the Rust struct has them non-Option.
type UpdateSourceInput struct {
	ID               uuid.UUID
	Name             *string
	Description      *string
	PipelineID       *uuid.UUID
	DatasetBranch    *string
	DatasetVersion   *int32
	PreviewLimit     int32
	DefaultMarking   string
	Status           string
	PropertyMappings json.RawMessage
	TriggerContext   json.RawMessage
}

// CreateRunInput mirrors `pub struct CreateRunInput`.
type CreateRunInput struct {
	ID           uuid.UUID
	SourceID     uuid.UUID
	ObjectTypeID uuid.UUID
	DatasetID    uuid.UUID
	PipelineID   *uuid.UUID
	TriggerType  string
	StartedBy    uuid.UUID
	Details      json.RawMessage
}

// CompleteRunInput mirrors `pub struct CompleteRunInput`.
type CompleteRunInput struct {
	ID            uuid.UUID
	SourceID      uuid.UUID
	PipelineRunID *uuid.UUID
	Status        string
	RowsRead      int32
	InsertedCount int32
	UpdatedCount  int32
	SkippedCount  int32
	ErrorCount    int32
	Details       json.RawMessage
	ErrorMessage  *string
	FinishedAt    time.Time
}

// ── Existence + lookup helpers ──────────────────────────────────────

const sourceColumns = `id, name, description, object_type_id, dataset_id, pipeline_id, dataset_branch,
                       dataset_version, preview_limit, default_marking, status, property_mappings,
                       trigger_context, owner_id, last_run_at, created_at, updated_at`

// DatasetExists mirrors `pub async fn dataset_exists`.
func DatasetExists(ctx context.Context, db *pgxpool.Pool, datasetID uuid.UUID) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM datasets WHERE id = $1)", datasetID).Scan(&exists)
	return exists, err
}

// PipelineExists mirrors `pub async fn pipeline_exists`.
func PipelineExists(ctx context.Context, db *pgxpool.Pool, pipelineID uuid.UUID) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pipelines WHERE id = $1)", pipelineID).Scan(&exists)
	return exists, err
}

// LoadSource mirrors `pub async fn load_source`.
func LoadSource(ctx context.Context, db *pgxpool.Pool, id uuid.UUID) (*models.OntologyFunnelSource, error) {
	var row models.OntologyFunnelSourceRow
	err := db.QueryRow(ctx,
		`SELECT `+sourceColumns+` FROM ontology_funnel_sources WHERE id = $1`, id,
	).Scan(
		&row.ID, &row.Name, &row.Description, &row.ObjectTypeID, &row.DatasetID,
		&row.PipelineID, &row.DatasetBranch, &row.DatasetVersion,
		&row.PreviewLimit, &row.DefaultMarking, &row.Status, &row.PropertyMappings,
		&row.TriggerContext, &row.OwnerID, &row.LastRunAt, &row.CreatedAt, &row.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := row.IntoSource()
	return &out, nil
}

// ListSources mirrors `pub async fn list_sources`.
func ListSources(ctx context.Context, db *pgxpool.Pool, params ListSourcesParams) ([]models.OntologyFunnelSource, error) {
	rows, err := db.Query(ctx,
		`SELECT `+sourceColumns+`
		 FROM ontology_funnel_sources
		 WHERE ($1::uuid IS NULL OR object_type_id = $1)
		   AND ($2 = '' OR status = $2)
		   AND ($3::boolean OR owner_id = $4)
		 ORDER BY created_at DESC
		 OFFSET $5 LIMIT $6`,
		params.ObjectTypeID, params.StatusFilter, params.IsAdmin, params.ActorID,
		params.Offset, params.Limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSources(rows)
}

// CountSources mirrors `pub async fn count_sources`.
func CountSources(
	ctx context.Context, db *pgxpool.Pool,
	objectTypeID *uuid.UUID, statusFilter string, isAdmin bool, actorID uuid.UUID,
) (int64, error) {
	var total int64
	err := db.QueryRow(ctx,
		`SELECT COUNT(*) FROM ontology_funnel_sources
		 WHERE ($1::uuid IS NULL OR object_type_id = $1)
		   AND ($2 = '' OR status = $2)
		   AND ($3::boolean OR owner_id = $4)`,
		objectTypeID, statusFilter, isAdmin, actorID,
	).Scan(&total)
	return total, err
}

// ListSourcesForHealth mirrors `pub async fn list_sources_for_health`.
func ListSourcesForHealth(ctx context.Context, db *pgxpool.Pool, params HealthSourcesParams) ([]models.OntologyFunnelSource, error) {
	rows, err := db.Query(ctx,
		`SELECT `+sourceColumns+`
		 FROM ontology_funnel_sources
		 WHERE ($1::uuid IS NULL OR object_type_id = $1)
		   AND ($2::boolean OR owner_id = $3)
		 ORDER BY created_at DESC`,
		params.ObjectTypeID, params.IsAdmin, params.ActorID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSources(rows)
}

func scanSources(rows pgx.Rows) ([]models.OntologyFunnelSource, error) {
	out := []models.OntologyFunnelSource{}
	for rows.Next() {
		var row models.OntologyFunnelSourceRow
		if err := rows.Scan(
			&row.ID, &row.Name, &row.Description, &row.ObjectTypeID, &row.DatasetID,
			&row.PipelineID, &row.DatasetBranch, &row.DatasetVersion,
			&row.PreviewLimit, &row.DefaultMarking, &row.Status, &row.PropertyMappings,
			&row.TriggerContext, &row.OwnerID, &row.LastRunAt, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, row.IntoSource())
	}
	return out, rows.Err()
}

// ── Source CRUD ─────────────────────────────────────────────────────

// CreateSource mirrors `pub async fn create_source`.
func CreateSource(ctx context.Context, db *pgxpool.Pool, input CreateSourceInput) (*models.OntologyFunnelSource, error) {
	mappings := input.PropertyMappings
	if len(mappings) == 0 {
		mappings = json.RawMessage(`[]`)
	}
	context := input.TriggerContext
	if len(context) == 0 {
		context = json.RawMessage(`{}`)
	}
	var row models.OntologyFunnelSourceRow
	err := db.QueryRow(ctx,
		`INSERT INTO ontology_funnel_sources (
			id, name, description, object_type_id, dataset_id, pipeline_id, dataset_branch,
			dataset_version, preview_limit, default_marking, status, property_mappings,
			trigger_context, owner_id
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13::jsonb, $14)
		RETURNING `+sourceColumns,
		input.ID, input.Name, input.Description, input.ObjectTypeID, input.DatasetID,
		input.PipelineID, input.DatasetBranch, input.DatasetVersion,
		input.PreviewLimit, input.DefaultMarking, input.Status, mappings,
		context, input.OwnerID,
	).Scan(
		&row.ID, &row.Name, &row.Description, &row.ObjectTypeID, &row.DatasetID,
		&row.PipelineID, &row.DatasetBranch, &row.DatasetVersion,
		&row.PreviewLimit, &row.DefaultMarking, &row.Status, &row.PropertyMappings,
		&row.TriggerContext, &row.OwnerID, &row.LastRunAt, &row.CreatedAt, &row.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	out := row.IntoSource()
	return &out, nil
}

// UpdateSource mirrors `pub async fn update_source`.
func UpdateSource(ctx context.Context, db *pgxpool.Pool, input UpdateSourceInput) (*models.OntologyFunnelSource, error) {
	mappings := input.PropertyMappings
	if len(mappings) == 0 {
		mappings = json.RawMessage(`[]`)
	}
	context := input.TriggerContext
	if len(context) == 0 {
		context = json.RawMessage(`{}`)
	}
	var row models.OntologyFunnelSourceRow
	err := db.QueryRow(ctx,
		`UPDATE ontology_funnel_sources
		 SET name = COALESCE($2, name),
		     description = COALESCE($3, description),
		     pipeline_id = $4,
		     dataset_branch = $5,
		     dataset_version = $6,
		     preview_limit = $7,
		     default_marking = $8,
		     status = $9,
		     property_mappings = $10::jsonb,
		     trigger_context = $11::jsonb,
		     updated_at = NOW()
		 WHERE id = $1
		 RETURNING `+sourceColumns,
		input.ID, input.Name, input.Description,
		input.PipelineID, input.DatasetBranch, input.DatasetVersion,
		input.PreviewLimit, input.DefaultMarking, input.Status,
		mappings, context,
	).Scan(
		&row.ID, &row.Name, &row.Description, &row.ObjectTypeID, &row.DatasetID,
		&row.PipelineID, &row.DatasetBranch, &row.DatasetVersion,
		&row.PreviewLimit, &row.DefaultMarking, &row.Status, &row.PropertyMappings,
		&row.TriggerContext, &row.OwnerID, &row.LastRunAt, &row.CreatedAt, &row.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := row.IntoSource()
	return &out, nil
}

// DeleteSource mirrors `pub async fn delete_source`.
func DeleteSource(ctx context.Context, db *pgxpool.Pool, id uuid.UUID) (bool, error) {
	ct, err := db.Exec(ctx, "DELETE FROM ontology_funnel_sources WHERE id = $1", id)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// ── Run lifecycle ───────────────────────────────────────────────────

// CreateRun mirrors `pub async fn create_run`. Appends the start
// event with the same JSON payload Rust emits.
func CreateRun(
	ctx context.Context,
	actions storage.ActionLogStore,
	tenant storage.TenantId,
	input CreateRunInput,
) error {
	startedAt := time.Now().UTC()
	payload, err := buildAppendPayload(input, startedAt)
	if err != nil {
		return err
	}
	return appendFunnelEvent(ctx, actions, tenant, input.SourceID, input.StartedBy.String(), payload, startedAt)
}

// CompleteRun mirrors `pub async fn complete_run`. Picks the right
// terminal event ("failed" status → funnel_run_failed; otherwise
// funnel_run_completed) and appends with finishedAt.
func CompleteRun(
	ctx context.Context,
	actions storage.ActionLogStore,
	tenant storage.TenantId,
	subject uuid.UUID,
	input CompleteRunInput,
) error {
	event := funnelRunCompletedEvt
	if input.Status == "failed" {
		event = funnelRunFailedEvent
	}
	payload, err := buildTerminalPayload(event, input)
	if err != nil {
		return err
	}
	return appendFunnelEvent(ctx, actions, tenant, input.SourceID, subject.String(), payload, input.FinishedAt)
}

// MarkSourceRan mirrors `pub async fn mark_source_ran`.
func MarkSourceRan(ctx context.Context, db *pgxpool.Pool, sourceID uuid.UUID, finishedAt time.Time) error {
	_, err := db.Exec(ctx,
		`UPDATE ontology_funnel_sources SET last_run_at = $2, updated_at = NOW() WHERE id = $1`,
		sourceID, finishedAt,
	)
	return err
}

// FailRun mirrors `pub async fn fail_run`. Synthesises a
// CompleteRunInput with status="failed", error_message=<error>, and
// the rest zeroed.
func FailRun(
	ctx context.Context,
	actions storage.ActionLogStore,
	tenant storage.TenantId,
	sourceID, runID, subject uuid.UUID,
	errMsg string,
) error {
	em := errMsg
	return CompleteRun(ctx, actions, tenant, subject, CompleteRunInput{
		ID:           runID,
		SourceID:     sourceID,
		Status:       "failed",
		ErrorCount:   1,
		Details:      json.RawMessage(`{}`),
		ErrorMessage: &em,
		FinishedAt:   time.Now().UTC(),
	})
}

// ── Run reads ───────────────────────────────────────────────────────

// LoadRun mirrors `pub async fn load_run`. Walks every action-log
// page (ListRecent), filters to entries whose run_id matches, folds
// into a single OntologyFunnelRun.
func LoadRun(
	ctx context.Context,
	actions storage.ActionLogStore,
	tenant storage.TenantId,
	runID uuid.UUID,
) (*models.OntologyFunnelRun, error) {
	events, err := loadFunnelEventsForRun(ctx, actions, tenant, runID)
	if err != nil {
		return nil, err
	}
	runs := runsFromEvents(events)
	if len(runs) == 0 {
		return nil, nil
	}
	return &runs[0], nil
}

// CountRunsForSource mirrors `pub async fn count_runs_for_source`.
func CountRunsForSource(
	ctx context.Context,
	actions storage.ActionLogStore,
	tenant storage.TenantId,
	sourceID uuid.UUID,
) (int64, error) {
	events, err := listFunnelEventsForSource(ctx, actions, tenant, sourceID)
	if err != nil {
		return 0, err
	}
	return int64(len(runsFromEvents(events))), nil
}

// ListRunsForSource mirrors `pub async fn list_runs_for_source`.
// Loads every event for the source, folds into runs, paginates the
// already-sorted slice (matches the Rust `skip().take()` cascade).
func ListRunsForSource(
	ctx context.Context,
	actions storage.ActionLogStore,
	tenant storage.TenantId,
	sourceID uuid.UUID,
	offset, limit int64,
) ([]models.OntologyFunnelRun, error) {
	events, err := listFunnelEventsForSource(ctx, actions, tenant, sourceID)
	if err != nil {
		return nil, err
	}
	runs := runsFromEvents(events)
	sortRunsDescByStarted(runs)
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	if int(offset) > len(runs) {
		return []models.OntologyFunnelRun{}, nil
	}
	end := int(offset) + int(limit)
	if end > len(runs) {
		end = len(runs)
	}
	return runs[offset:end], nil
}

// LoadRunForSource mirrors `pub async fn load_run_for_source`.
func LoadRunForSource(
	ctx context.Context,
	actions storage.ActionLogStore,
	tenant storage.TenantId,
	sourceID, runID uuid.UUID,
) (*models.OntologyFunnelRun, error) {
	events, err := listFunnelEventsForSource(ctx, actions, tenant, sourceID)
	if err != nil {
		return nil, err
	}
	filtered := events[:0]
	for _, ev := range events {
		if ev.payload.RunID == runID {
			filtered = append(filtered, ev)
		}
	}
	runs := runsFromEvents(filtered)
	if len(runs) == 0 {
		return nil, nil
	}
	return &runs[0], nil
}

// LoadHealthMetrics mirrors `pub async fn load_health_metrics`.
// Computes the same aggregations: percentile_cont 0.95, success/
// failed/warning counts, last_*_at timestamps, totals.
func LoadHealthMetrics(
	ctx context.Context,
	actions storage.ActionLogStore,
	tenant storage.TenantId,
	sourceID uuid.UUID,
) (models.OntologyFunnelHealthMetricsRow, error) {
	events, err := listFunnelEventsForSource(ctx, actions, tenant, sourceID)
	if err != nil {
		return models.OntologyFunnelHealthMetricsRow{}, err
	}
	runs := runsFromEvents(events)
	sortRunsDescByStarted(runs)

	now := time.Now().UTC()
	durations := make([]float64, 0, len(runs))
	for _, run := range runs {
		end := now
		if run.FinishedAt != nil {
			end = *run.FinishedAt
		}
		ms := end.Sub(run.StartedAt).Milliseconds()
		if ms < 0 {
			ms = 0
		}
		durations = append(durations, float64(ms))
	}
	sort.Float64s(durations)

	p95 := percentileCont(durations, 0.95)
	var avg *float64
	if len(durations) > 0 {
		var sum float64
		for _, d := range durations {
			sum += d
		}
		v := sum / float64(len(durations))
		avg = &v
	}
	var maxDur *int64
	if len(durations) > 0 {
		v := int64(durations[len(durations)-1])
		maxDur = &v
	}

	totalRuns := int64(len(runs))
	successful, failed, warning := int64(0), int64(0), int64(0)
	for _, run := range runs {
		switch run.Status {
		case "completed", "dry_run":
			successful++
		case "failed":
			failed++
		case "completed_with_errors", "dry_run_with_errors":
			warning++
		}
	}

	var latestStatus *string
	if len(runs) > 0 {
		s := runs[0].Status
		latestStatus = &s
	}

	out := models.OntologyFunnelHealthMetricsRow{
		TotalRuns:       totalRuns,
		SuccessfulRuns:  successful,
		FailedRuns:      failed,
		WarningRuns:     warning,
		AvgDurationMs:   avg,
		P95DurationMs:   p95,
		MaxDurationMs:   maxDur,
		LatestRunStatus: latestStatus,
	}

	for _, run := range runs {
		end := run.FinishedAt
		if end == nil {
			s := run.StartedAt
			end = &s
		}
		if out.LastRunAt == nil || end.After(*out.LastRunAt) {
			tt := *end
			out.LastRunAt = &tt
		}
		switch run.Status {
		case "completed", "dry_run":
			if run.FinishedAt != nil && (out.LastSuccessAt == nil || run.FinishedAt.After(*out.LastSuccessAt)) {
				tt := *run.FinishedAt
				out.LastSuccessAt = &tt
			}
		case "failed":
			if run.FinishedAt != nil && (out.LastFailureAt == nil || run.FinishedAt.After(*out.LastFailureAt)) {
				tt := *run.FinishedAt
				out.LastFailureAt = &tt
			}
		case "completed_with_errors", "dry_run_with_errors":
			if run.FinishedAt != nil && (out.LastWarningAt == nil || run.FinishedAt.After(*out.LastWarningAt)) {
				tt := *run.FinishedAt
				out.LastWarningAt = &tt
			}
		}
		out.RowsRead += int64(run.RowsRead)
		out.InsertedCount += int64(run.InsertedCount)
		out.UpdatedCount += int64(run.UpdatedCount)
		out.SkippedCount += int64(run.SkippedCount)
		out.ErrorCount += int64(run.ErrorCount)
	}
	return out, nil
}

// ── Internal helpers (1:1 with the Rust private fns) ────────────────

func buildAppendPayload(input CreateRunInput, startedAt time.Time) (json.RawMessage, error) {
	details := input.Details
	if len(details) == 0 {
		details = json.RawMessage(`null`)
	}
	return json.Marshal(map[string]any{
		"event":          funnelRunStartedEvent,
		"run_id":         input.ID,
		"source_id":      input.SourceID,
		"object_type_id": input.ObjectTypeID,
		"dataset_id":     input.DatasetID,
		"pipeline_id":    input.PipelineID,
		"status":         "running",
		"trigger_type":   input.TriggerType,
		"started_by":     input.StartedBy,
		"details":        json.RawMessage(details),
		"started_at":     startedAt.Format(time.RFC3339Nano),
	})
}

func buildTerminalPayload(event string, input CompleteRunInput) (json.RawMessage, error) {
	details := input.Details
	if len(details) == 0 {
		details = json.RawMessage(`null`)
	}
	return json.Marshal(map[string]any{
		"event":           event,
		"run_id":          input.ID,
		"source_id":       input.SourceID,
		"pipeline_run_id": input.PipelineRunID,
		"status":          input.Status,
		"rows_read":       input.RowsRead,
		"inserted_count":  input.InsertedCount,
		"updated_count":   input.UpdatedCount,
		"skipped_count":   input.SkippedCount,
		"error_count":     input.ErrorCount,
		"details":         json.RawMessage(details),
		"error_message":   input.ErrorMessage,
		"finished_at":     input.FinishedAt.Format(time.RFC3339Nano),
	})
}

func appendFunnelEvent(
	ctx context.Context,
	actions storage.ActionLogStore,
	tenant storage.TenantId,
	sourceID uuid.UUID,
	subject string,
	payload json.RawMessage,
	recordedAt time.Time,
) error {
	objID := storage.ObjectId(sourceID.String())
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("uuid v7: %w", err)
	}
	return actions.Append(ctx, storage.ActionLogEntry{
		Tenant:       tenant,
		ActionID:     id.String(),
		Kind:         funnelRunKind,
		Subject:      subject,
		Object:       &objID,
		Payload:      payload,
		RecordedAtMs: recordedAt.UnixMilli(),
	})
}

func listFunnelEventsForSource(
	ctx context.Context,
	actions storage.ActionLogStore,
	tenant storage.TenantId,
	sourceID uuid.UUID,
) ([]eventWithTime, error) {
	objID := storage.ObjectId(sourceID.String())
	var token *string
	out := []eventWithTime{}
	for {
		page, err := actions.ListForObject(
			ctx, tenant, objID,
			storage.Page{Size: actionLogScanPageSize, Token: token},
			storage.Strong(),
		)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			payload, recordedAt := decodeFunnelEvent(item)
			if payload != nil && recordedAt != nil {
				out = append(out, eventWithTime{payload: *payload, recordedAt: *recordedAt})
			}
		}
		if page.NextToken == nil {
			return out, nil
		}
		token = page.NextToken
	}
}

func loadFunnelEventsForRun(
	ctx context.Context,
	actions storage.ActionLogStore,
	tenant storage.TenantId,
	runID uuid.UUID,
) ([]eventWithTime, error) {
	var token *string
	out := []eventWithTime{}
	for {
		page, err := actions.ListRecent(
			ctx, tenant,
			storage.Page{Size: actionLogScanPageSize, Token: token},
			storage.Strong(),
		)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			payload, recordedAt := decodeFunnelEvent(item)
			if payload == nil || recordedAt == nil {
				continue
			}
			if payload.RunID != runID {
				continue
			}
			out = append(out, eventWithTime{payload: *payload, recordedAt: *recordedAt})
			if payload.Event != nil && *payload.Event == funnelRunStartedEvent {
				return out, nil
			}
		}
		if page.NextToken == nil {
			return out, nil
		}
		token = page.NextToken
	}
}

// percentileCont mirrors the Rust private helper. Linear
// interpolation between the two ranks, matching Postgres'
// percentile_cont aggregate. Returns nil for empty input.
func percentileCont(sorted []float64, percentile float64) *float64 {
	if len(sorted) == 0 {
		return nil
	}
	if len(sorted) == 1 {
		v := sorted[0]
		return &v
	}
	if percentile < 0 {
		percentile = 0
	}
	if percentile > 1 {
		percentile = 1
	}
	rank := percentile * float64(len(sorted)-1)
	lower := int(rank)
	upper := lower
	if rank > float64(lower) {
		upper = lower + 1
	}
	if upper >= len(sorted) {
		upper = len(sorted) - 1
	}
	if lower == upper {
		v := sorted[lower]
		return &v
	}
	low := sorted[lower]
	high := sorted[upper]
	v := low + (high-low)*(rank-float64(lower))
	return &v
}

// sortRunsDescByStarted matches Rust `runs_from_events`'s tail
// sort: started_at DESC, then id DESC. Used by the public LIST
// endpoints + LoadHealthMetrics.
func sortRunsDescByStarted(runs []models.OntologyFunnelRun) {
	sort.SliceStable(runs, func(i, j int) bool {
		if !runs[i].StartedAt.Equal(runs[j].StartedAt) {
			return runs[i].StartedAt.After(runs[j].StartedAt)
		}
		return runs[i].ID.String() > runs[j].ID.String()
	})
}
