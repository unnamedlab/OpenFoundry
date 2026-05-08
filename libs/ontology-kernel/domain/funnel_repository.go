// Funnel run-ledger reconstructor.
//
// Mirrors the subset of `libs/ontology-kernel/src/domain/funnel_repository.rs`
// the storage handler exercises today: `ListRunsForTenant` plus the
// supporting payload decoder and per-run accumulator. Funnel source
// CRUD (the bulk of the Rust file, ~700 LOC) lives behind
// `handlers/funnel`; it lands when that bounded context is ported.
//
// The semantics are byte-identical to Rust:
//   - kind filter is `"funnel_run"` exactly,
//   - events fold left-to-right by `run_id`, started → terminal,
//   - default trigger_type when only a started event lands is "manual",
//   - default status when no terminal event lands is "running".
package domain

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

const (
	funnelRunKind          = "funnel_run"
	funnelRunStartedEvent  = "funnel_run_started"
	funnelRunCompletedEvt  = "funnel_run_completed"
	funnelRunFailedEvent   = "funnel_run_failed"
	actionLogScanPageSize  = uint32(5_000)
)

// FunnelRunEventPayload mirrors the Rust private struct of the same
// name: the JSON shape persisted as `payload` on every action-log
// entry of `kind = "funnel_run"`.
type FunnelRunEventPayload struct {
	Event          *string         `json:"event,omitempty"`
	RunID          uuid.UUID       `json:"run_id"`
	SourceID       uuid.UUID       `json:"source_id"`
	ObjectTypeID   *uuid.UUID      `json:"object_type_id,omitempty"`
	DatasetID      *uuid.UUID      `json:"dataset_id,omitempty"`
	PipelineID     *uuid.UUID      `json:"pipeline_id,omitempty"`
	PipelineRunID  *uuid.UUID      `json:"pipeline_run_id,omitempty"`
	Status         *string         `json:"status,omitempty"`
	TriggerType    *string         `json:"trigger_type,omitempty"`
	StartedBy      *uuid.UUID      `json:"started_by,omitempty"`
	RowsRead       *int32          `json:"rows_read,omitempty"`
	InsertedCount  *int32          `json:"inserted_count,omitempty"`
	UpdatedCount   *int32          `json:"updated_count,omitempty"`
	SkippedCount   *int32          `json:"skipped_count,omitempty"`
	ErrorCount     *int32          `json:"error_count,omitempty"`
	Details        json.RawMessage `json:"details,omitempty"`
	ErrorMessage   *string         `json:"error_message,omitempty"`
	StartedAt      *time.Time      `json:"started_at,omitempty"`
	FinishedAt     *time.Time      `json:"finished_at,omitempty"`
}

// funnelRunAccumulator mirrors the Rust private struct.
type funnelRunAccumulator struct {
	id             uuid.UUID
	sourceID       uuid.UUID
	objectTypeID   *uuid.UUID
	datasetID      *uuid.UUID
	pipelineID     *uuid.UUID
	pipelineRunID  *uuid.UUID
	status         *string
	triggerType    *string
	startedBy      *uuid.UUID
	rowsRead       int32
	insertedCount  int32
	updatedCount   int32
	skippedCount   int32
	errorCount     int32
	details        json.RawMessage
	errorMessage   *string
	startedAt      *time.Time
	finishedAt     *time.Time
}

func newFunnelRunAccumulator(id, sourceID uuid.UUID) *funnelRunAccumulator {
	return &funnelRunAccumulator{id: id, sourceID: sourceID}
}

// apply mirrors the Rust `impl FunnelRunAccumulator { fn apply }`.
// Carries the same `or_else` cascades, the same special-cases for
// started / completed / failed events, and the same defaults.
func (a *funnelRunAccumulator) apply(payload FunnelRunEventPayload, recordedAt time.Time) {
	if a.objectTypeID == nil {
		a.objectTypeID = payload.ObjectTypeID
	}
	if a.datasetID == nil {
		a.datasetID = payload.DatasetID
	}
	if a.pipelineID == nil {
		a.pipelineID = payload.PipelineID
	}
	if a.pipelineRunID == nil {
		a.pipelineRunID = payload.PipelineRunID
	}
	if a.triggerType == nil {
		a.triggerType = payload.TriggerType
	}
	if a.startedBy == nil {
		a.startedBy = payload.StartedBy
	}

	event := ""
	if payload.Event != nil {
		event = *payload.Event
	}

	switch event {
	case funnelRunStartedEvent:
		if a.status == nil {
			s := "running"
			if payload.Status != nil {
				s = *payload.Status
			}
			a.status = &s
		}
		if isJSONNull(a.details) && len(payload.Details) > 0 {
			a.details = payload.Details
		}
		if a.startedAt == nil {
			if payload.StartedAt != nil {
				a.startedAt = payload.StartedAt
			} else {
				rt := recordedAt
				a.startedAt = &rt
			}
		}
	case funnelRunCompletedEvt, funnelRunFailedEvent:
		if payload.Status != nil {
			a.status = payload.Status
		} else if event == funnelRunFailedEvent {
			s := "failed"
			a.status = &s
		}
		if payload.RowsRead != nil {
			a.rowsRead = *payload.RowsRead
		}
		if payload.InsertedCount != nil {
			a.insertedCount = *payload.InsertedCount
		}
		if payload.UpdatedCount != nil {
			a.updatedCount = *payload.UpdatedCount
		}
		if payload.SkippedCount != nil {
			a.skippedCount = *payload.SkippedCount
		}
		if payload.ErrorCount != nil {
			a.errorCount = *payload.ErrorCount
		}
		if len(payload.Details) > 0 {
			a.details = payload.Details
		}
		if payload.ErrorMessage != nil {
			a.errorMessage = payload.ErrorMessage
		}
		if a.finishedAt == nil {
			if payload.FinishedAt != nil {
				a.finishedAt = payload.FinishedAt
			} else {
				rt := recordedAt
				a.finishedAt = &rt
			}
		}
	default:
		if payload.Status != nil && a.status == nil {
			a.status = payload.Status
		}
		if a.startedAt == nil {
			a.startedAt = payload.StartedAt
		}
		if a.finishedAt == nil {
			a.finishedAt = payload.FinishedAt
		}
		if isJSONNull(a.details) && len(payload.Details) > 0 {
			a.details = payload.Details
		}
	}
}

// intoRun mirrors `impl FunnelRunAccumulator { fn into_run }`.
// Returns nil for accumulators that never resolved an object_type_id,
// dataset_id or started_at — same `Option`-returning Rust shape.
func (a *funnelRunAccumulator) intoRun() *models.OntologyFunnelRun {
	if a.objectTypeID == nil || a.datasetID == nil || a.startedAt == nil {
		return nil
	}
	status := "running"
	if a.status != nil {
		status = *a.status
	}
	triggerType := "manual"
	if a.triggerType != nil {
		triggerType = *a.triggerType
	}
	return &models.OntologyFunnelRun{
		ID:            a.id,
		SourceID:      a.sourceID,
		ObjectTypeID:  *a.objectTypeID,
		DatasetID:     *a.datasetID,
		PipelineID:    a.pipelineID,
		PipelineRunID: a.pipelineRunID,
		Status:        status,
		TriggerType:   triggerType,
		StartedBy:     a.startedBy,
		RowsRead:      a.rowsRead,
		InsertedCount: a.insertedCount,
		UpdatedCount:  a.updatedCount,
		SkippedCount:  a.skippedCount,
		ErrorCount:    a.errorCount,
		Details:       a.details,
		ErrorMessage:  a.errorMessage,
		StartedAt:     *a.startedAt,
		FinishedAt:    a.finishedAt,
	}
}

func isJSONNull(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	for _, b := range raw {
		if b != ' ' && b != '\t' && b != '\n' && b != '\r' {
			return string(raw) == "null"
		}
	}
	return true
}

// decodeFunnelEvent mirrors `fn decode_funnel_event`.
//
// Returns nil values when the entry is not of the funnel kind or the
// payload is not a valid funnel-run event — both branches the Rust
// `Option`-typed return collapses into.
func decodeFunnelEvent(entry storage.ActionLogEntry) (*FunnelRunEventPayload, *time.Time) {
	if entry.Kind != funnelRunKind {
		return nil, nil
	}
	recordedAt := time.UnixMilli(entry.RecordedAtMs).UTC()
	var payload FunnelRunEventPayload
	if err := json.Unmarshal(entry.Payload, &payload); err != nil {
		return nil, nil
	}
	return &payload, &recordedAt
}

// runsFromEvents mirrors `fn runs_from_events`. Folds events by
// run_id and emits one `OntologyFunnelRun` per accumulator that
// finalises (i.e. carries the minimum fields).
func runsFromEvents(events []eventWithTime) []models.OntologyFunnelRun {
	byRun := map[uuid.UUID]*funnelRunAccumulator{}
	// Preserve insertion order by remembering the order in which
	// run_ids first appear, exactly like the Rust `HashMap` +
	// stable-iteration is fed to the response.
	var order []uuid.UUID
	for _, ev := range events {
		acc, ok := byRun[ev.payload.RunID]
		if !ok {
			acc = newFunnelRunAccumulator(ev.payload.RunID, ev.payload.SourceID)
			byRun[ev.payload.RunID] = acc
			order = append(order, ev.payload.RunID)
		}
		acc.apply(ev.payload, ev.recordedAt)
	}
	out := make([]models.OntologyFunnelRun, 0, len(order))
	for _, id := range order {
		if run := byRun[id].intoRun(); run != nil {
			out = append(out, *run)
		}
	}
	return out
}

type eventWithTime struct {
	payload    FunnelRunEventPayload
	recordedAt time.Time
}

// ListFunnelEventsForTenant mirrors `async fn list_funnel_events_for_tenant`.
// Pages through every action-log entry of the tenant filtering the
// `funnel_run` kind. Surfaces the raw RepoError so the caller can
// distinguish backend errors from decoding errors.
func ListFunnelEventsForTenant(
	ctx context.Context,
	actions storage.ActionLogStore,
	tenant storage.TenantId,
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

// ListRunsForTenant mirrors `pub async fn list_runs_for_tenant`.
// Stitches every funnel-run event in the tenant's action log into
// an ordered list of `OntologyFunnelRun`.
func ListRunsForTenant(
	ctx context.Context,
	actions storage.ActionLogStore,
	tenant storage.TenantId,
) ([]models.OntologyFunnelRun, error) {
	events, err := ListFunnelEventsForTenant(ctx, actions, tenant)
	if err != nil {
		return nil, err
	}
	return runsFromEvents(events), nil
}
