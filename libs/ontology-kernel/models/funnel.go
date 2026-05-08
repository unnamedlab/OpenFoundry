package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// libs/ontology-kernel/src/models/funnel.rs default helpers.
func defaultFunnelPreviewLimit() int32  { return 500 }
func defaultFunnelStatus() string       { return "active" }
func defaultFunnelMarking() string      { return "public" }

// NormalizePreviewLimit mirrors `fn normalize_preview_limit(value)`
// — defaults to 500 then clamps to [1, 1000].
func NormalizePreviewLimit(value *int32) int32 {
	v := defaultFunnelPreviewLimit()
	if value != nil {
		v = *value
	}
	if v < 1 {
		v = 1
	}
	if v > 1000 {
		v = 1000
	}
	return v
}

// NormalizeFunnelStatus mirrors `fn normalize_funnel_status(value)` —
// defaults to "active".
func NormalizeFunnelStatus(value *string) string {
	if value == nil {
		return defaultFunnelStatus()
	}
	return *value
}

// NormalizeDefaultMarking mirrors `fn normalize_default_marking(value)`
// — defaults to "public".
func NormalizeDefaultMarking(value *string) string {
	if value == nil {
		return defaultFunnelMarking()
	}
	return *value
}

// NormalizeStaleAfterHours mirrors `fn normalize_stale_after_hours` —
// defaults to 24 then clamps to [1, 24*30].
func NormalizeStaleAfterHours(value *int64) int64 {
	v := int64(24)
	if value != nil {
		v = *value
	}
	if v < 1 {
		v = 1
	}
	max := int64(24 * 30)
	if v > max {
		v = max
	}
	return v
}

// OntologyFunnelPropertyMapping mirrors `struct OntologyFunnelPropertyMapping`.
type OntologyFunnelPropertyMapping struct {
	SourceField    string `json:"source_field"`
	TargetProperty string `json:"target_property"`
}

// OntologyFunnelSourceRow mirrors `struct OntologyFunnelSourceRow`.
type OntologyFunnelSourceRow struct {
	ID               uuid.UUID       `db:"id"`
	Name             string          `db:"name"`
	Description      string          `db:"description"`
	ObjectTypeID     uuid.UUID       `db:"object_type_id"`
	DatasetID        uuid.UUID       `db:"dataset_id"`
	PipelineID       *uuid.UUID      `db:"pipeline_id"`
	DatasetBranch    *string         `db:"dataset_branch"`
	DatasetVersion   *int32          `db:"dataset_version"`
	PreviewLimit     int32           `db:"preview_limit"`
	DefaultMarking   string          `db:"default_marking"`
	Status           string          `db:"status"`
	PropertyMappings json.RawMessage `db:"property_mappings"`
	TriggerContext   json.RawMessage `db:"trigger_context"`
	OwnerID          uuid.UUID       `db:"owner_id"`
	LastRunAt        *time.Time      `db:"last_run_at"`
	CreatedAt        time.Time       `db:"created_at"`
	UpdatedAt        time.Time       `db:"updated_at"`
}

// OntologyFunnelSource mirrors `struct OntologyFunnelSource`.
type OntologyFunnelSource struct {
	ID               uuid.UUID                       `json:"id"`
	Name             string                          `json:"name"`
	Description      string                          `json:"description"`
	ObjectTypeID     uuid.UUID                       `json:"object_type_id"`
	DatasetID        uuid.UUID                       `json:"dataset_id"`
	PipelineID       *uuid.UUID                      `json:"pipeline_id"`
	DatasetBranch    *string                         `json:"dataset_branch"`
	DatasetVersion   *int32                          `json:"dataset_version"`
	PreviewLimit     int32                           `json:"preview_limit"`
	DefaultMarking   string                          `json:"default_marking"`
	Status           string                          `json:"status"`
	PropertyMappings []OntologyFunnelPropertyMapping `json:"property_mappings"`
	TriggerContext   json.RawMessage                 `json:"trigger_context"`
	OwnerID          uuid.UUID                       `json:"owner_id"`
	LastRunAt        *time.Time                      `json:"last_run_at"`
	CreatedAt        time.Time                       `json:"created_at"`
	UpdatedAt        time.Time                       `json:"updated_at"`
}

// IntoSource mirrors `TryFrom<OntologyFunnelSourceRow>`. Property
// mappings parse failures fall back to empty slice (`unwrap_or_default`).
func (row OntologyFunnelSourceRow) IntoSource() OntologyFunnelSource {
	mappings := []OntologyFunnelPropertyMapping{}
	if len(row.PropertyMappings) > 0 {
		_ = json.Unmarshal(row.PropertyMappings, &mappings)
	}
	return OntologyFunnelSource{
		ID:               row.ID,
		Name:             row.Name,
		Description:      row.Description,
		ObjectTypeID:     row.ObjectTypeID,
		DatasetID:        row.DatasetID,
		PipelineID:       row.PipelineID,
		DatasetBranch:    row.DatasetBranch,
		DatasetVersion:   row.DatasetVersion,
		PreviewLimit:     row.PreviewLimit,
		DefaultMarking:   row.DefaultMarking,
		Status:           row.Status,
		PropertyMappings: mappings,
		TriggerContext:   row.TriggerContext,
		OwnerID:          row.OwnerID,
		LastRunAt:        row.LastRunAt,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

// OntologyFunnelRun mirrors `struct OntologyFunnelRun`.
type OntologyFunnelRun struct {
	ID            uuid.UUID       `json:"id"               db:"id"`
	SourceID      uuid.UUID       `json:"source_id"        db:"source_id"`
	ObjectTypeID  uuid.UUID       `json:"object_type_id"   db:"object_type_id"`
	DatasetID     uuid.UUID       `json:"dataset_id"       db:"dataset_id"`
	PipelineID    *uuid.UUID      `json:"pipeline_id"      db:"pipeline_id"`
	PipelineRunID *uuid.UUID      `json:"pipeline_run_id"  db:"pipeline_run_id"`
	Status        string          `json:"status"           db:"status"`
	TriggerType   string          `json:"trigger_type"     db:"trigger_type"`
	StartedBy     *uuid.UUID      `json:"started_by"       db:"started_by"`
	RowsRead      int32           `json:"rows_read"        db:"rows_read"`
	InsertedCount int32           `json:"inserted_count"   db:"inserted_count"`
	UpdatedCount  int32           `json:"updated_count"    db:"updated_count"`
	SkippedCount  int32           `json:"skipped_count"    db:"skipped_count"`
	ErrorCount    int32           `json:"error_count"      db:"error_count"`
	Details       json.RawMessage `json:"details"          db:"details"`
	ErrorMessage  *string         `json:"error_message"    db:"error_message"`
	StartedAt     time.Time       `json:"started_at"       db:"started_at"`
	FinishedAt    *time.Time      `json:"finished_at"      db:"finished_at"`
}

// CreateOntologyFunnelSourceRequest mirrors the same struct.
type CreateOntologyFunnelSourceRequest struct {
	Name             string                            `json:"name"`
	Description      *string                           `json:"description,omitempty"`
	ObjectTypeID     uuid.UUID                         `json:"object_type_id"`
	DatasetID        uuid.UUID                         `json:"dataset_id"`
	PipelineID       *uuid.UUID                        `json:"pipeline_id,omitempty"`
	DatasetBranch    *string                           `json:"dataset_branch,omitempty"`
	DatasetVersion   *int32                            `json:"dataset_version,omitempty"`
	PreviewLimit     *int32                            `json:"preview_limit,omitempty"`
	DefaultMarking   *string                           `json:"default_marking,omitempty"`
	Status           *string                           `json:"status,omitempty"`
	PropertyMappings *[]OntologyFunnelPropertyMapping  `json:"property_mappings,omitempty"`
	TriggerContext   json.RawMessage                   `json:"trigger_context,omitempty"`
}

// UpdateOntologyFunnelSourceRequest mirrors the same struct.
//
// `pipeline_id`, `dataset_branch`, `dataset_version` carry the Rust
// `Option<Option<T>>` three-way semantics. Implemented via the
// `*XxxUpdate` carriers + parent-level UnmarshalJSON over the raw
// map.
type UpdateOntologyFunnelSourceRequest struct {
	Name             *string                          `json:"name,omitempty"`
	Description      *string                          `json:"description,omitempty"`
	PipelineID       *UUIDUpdate                      `json:"-"`
	DatasetBranch    *StringUpdate                    `json:"-"`
	DatasetVersion   *Int32Update                     `json:"-"`
	PreviewLimit     *int32                           `json:"preview_limit,omitempty"`
	DefaultMarking   *string                          `json:"default_marking,omitempty"`
	Status           *string                          `json:"status,omitempty"`
	PropertyMappings *[]OntologyFunnelPropertyMapping `json:"property_mappings,omitempty"`
	TriggerContext   json.RawMessage                  `json:"trigger_context,omitempty"`
}

// UUIDUpdate carries `Option<Option<Uuid>>` three-way semantics.
type UUIDUpdate struct {
	Value *uuid.UUID
}

// Int32Update carries `Option<Option<i32>>` three-way semantics.
type Int32Update struct {
	Value *int32
}

// UnmarshalJSON detects key presence for the three Option<Option<T>>
// fields.
func (r *UpdateOntologyFunnelSourceRequest) UnmarshalJSON(b []byte) error {
	type alias UpdateOntologyFunnelSourceRequest
	if err := json.Unmarshal(b, (*alias)(r)); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if v, ok := raw["pipeline_id"]; ok {
		upd := &UUIDUpdate{}
		if string(v) != "null" {
			var id uuid.UUID
			if err := json.Unmarshal(v, &id); err != nil {
				return err
			}
			upd.Value = &id
		}
		r.PipelineID = upd
	}
	if v, ok := raw["dataset_branch"]; ok {
		upd := &StringUpdate{}
		if string(v) != "null" {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return err
			}
			upd.Value = &s
		}
		r.DatasetBranch = upd
	}
	if v, ok := raw["dataset_version"]; ok {
		upd := &Int32Update{}
		if string(v) != "null" {
			var n int32
			if err := json.Unmarshal(v, &n); err != nil {
				return err
			}
			upd.Value = &n
		}
		r.DatasetVersion = upd
	}
	return nil
}

// MarshalJSON: emit absent (omit), null (clear) or value for the
// Option<Option<T>> fields, plus the rest via the alias path.
func (r UpdateOntologyFunnelSourceRequest) MarshalJSON() ([]byte, error) {
	type alias UpdateOntologyFunnelSourceRequest
	base, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	bag := map[string]json.RawMessage{}
	if err := json.Unmarshal(base, &bag); err != nil {
		return nil, err
	}
	emit := func(key string, present bool, value any, isClear bool) error {
		if !present {
			return nil
		}
		if isClear {
			bag[key] = json.RawMessage("null")
			return nil
		}
		v, err := json.Marshal(value)
		if err != nil {
			return err
		}
		bag[key] = v
		return nil
	}
	if r.PipelineID != nil {
		if err := emit("pipeline_id", true, r.PipelineID.Value, r.PipelineID.Value == nil); err != nil {
			return nil, err
		}
	}
	if r.DatasetBranch != nil {
		if r.DatasetBranch.Value == nil {
			bag["dataset_branch"] = json.RawMessage("null")
		} else {
			v, _ := json.Marshal(*r.DatasetBranch.Value)
			bag["dataset_branch"] = v
		}
	}
	if r.DatasetVersion != nil {
		if r.DatasetVersion.Value == nil {
			bag["dataset_version"] = json.RawMessage("null")
		} else {
			v, _ := json.Marshal(*r.DatasetVersion.Value)
			bag["dataset_version"] = v
		}
	}
	return json.Marshal(bag)
}

// ListOntologyFunnelSourcesQuery mirrors the same struct.
type ListOntologyFunnelSourcesQuery struct {
	ObjectTypeID *uuid.UUID `json:"object_type_id,omitempty"`
	Status       *string    `json:"status,omitempty"`
	Page         *int64     `json:"page,omitempty"`
	PerPage      *int64     `json:"per_page,omitempty"`
}

// ListOntologyFunnelSourcesResponse mirrors the same struct.
type ListOntologyFunnelSourcesResponse struct {
	Data    []OntologyFunnelSource `json:"data"`
	Total   int64                  `json:"total"`
	Page    int64                  `json:"page"`
	PerPage int64                  `json:"per_page"`
}

// TriggerOntologyFunnelRunRequest mirrors `struct TriggerOntologyFunnelRunRequest`.
type TriggerOntologyFunnelRunRequest struct {
	Limit          *int32          `json:"limit,omitempty"`
	DatasetBranch  *string         `json:"dataset_branch,omitempty"`
	DatasetVersion *int32          `json:"dataset_version,omitempty"`
	SkipPipeline   bool            `json:"skip_pipeline"`
	DryRun         bool            `json:"dry_run"`
	TriggerContext json.RawMessage `json:"trigger_context,omitempty"`
}

// ListOntologyFunnelRunsQuery mirrors the same struct.
type ListOntologyFunnelRunsQuery struct {
	Page    *int64 `json:"page,omitempty"`
	PerPage *int64 `json:"per_page,omitempty"`
}

// ListOntologyFunnelRunsResponse mirrors the same struct.
type ListOntologyFunnelRunsResponse struct {
	Data    []OntologyFunnelRun `json:"data"`
	Total   int64               `json:"total"`
	Page    int64               `json:"page"`
	PerPage int64               `json:"per_page"`
}

// ListOntologyFunnelHealthQuery mirrors the same struct.
type ListOntologyFunnelHealthQuery struct {
	ObjectTypeID    *uuid.UUID `json:"object_type_id,omitempty"`
	StaleAfterHours *int64     `json:"stale_after_hours,omitempty"`
}

// GetOntologyFunnelSourceHealthQuery mirrors the same struct.
type GetOntologyFunnelSourceHealthQuery struct {
	StaleAfterHours *int64 `json:"stale_after_hours,omitempty"`
}

// OntologyFunnelHealthMetricsRow mirrors `struct OntologyFunnelHealthMetricsRow`.
type OntologyFunnelHealthMetricsRow struct {
	TotalRuns        int64      `db:"total_runs"`
	SuccessfulRuns   int64      `db:"successful_runs"`
	FailedRuns       int64      `db:"failed_runs"`
	WarningRuns      int64      `db:"warning_runs"`
	AvgDurationMs    *float64   `db:"avg_duration_ms"`
	P95DurationMs    *float64   `db:"p95_duration_ms"`
	MaxDurationMs    *int64     `db:"max_duration_ms"`
	LatestRunStatus  *string    `db:"latest_run_status"`
	LastRunAt        *time.Time `db:"last_run_at"`
	LastSuccessAt    *time.Time `db:"last_success_at"`
	LastFailureAt    *time.Time `db:"last_failure_at"`
	LastWarningAt    *time.Time `db:"last_warning_at"`
	RowsRead         int64      `db:"rows_read"`
	InsertedCount    int64      `db:"inserted_count"`
	UpdatedCount     int64      `db:"updated_count"`
	SkippedCount     int64      `db:"skipped_count"`
	ErrorCount       int64      `db:"error_count"`
}

// OntologyFunnelSourceHealth mirrors `struct OntologyFunnelSourceHealth`.
type OntologyFunnelSourceHealth struct {
	Source          OntologyFunnelSource `json:"source"`
	HealthStatus    string               `json:"health_status"`
	HealthReason    string               `json:"health_reason"`
	TotalRuns       int64                `json:"total_runs"`
	SuccessfulRuns  int64                `json:"successful_runs"`
	FailedRuns      int64                `json:"failed_runs"`
	WarningRuns     int64                `json:"warning_runs"`
	SuccessRate     float64              `json:"success_rate"`
	AvgDurationMs   *float64             `json:"avg_duration_ms"`
	P95DurationMs   *float64             `json:"p95_duration_ms"`
	MaxDurationMs   *int64               `json:"max_duration_ms"`
	LatestRunStatus *string              `json:"latest_run_status"`
	LastRunAt       *time.Time           `json:"last_run_at"`
	LastSuccessAt   *time.Time           `json:"last_success_at"`
	LastFailureAt   *time.Time           `json:"last_failure_at"`
	LastWarningAt   *time.Time           `json:"last_warning_at"`
	RowsRead        int64                `json:"rows_read"`
	InsertedCount   int64                `json:"inserted_count"`
	UpdatedCount    int64                `json:"updated_count"`
	SkippedCount    int64                `json:"skipped_count"`
	ErrorCount      int64                `json:"error_count"`
}

// OntologyFunnelHealthResponse mirrors `struct OntologyFunnelHealthResponse`.
type OntologyFunnelHealthResponse struct {
	StaleAfterHours    int64                        `json:"stale_after_hours"`
	TotalSources       int64                        `json:"total_sources"`
	ActiveSources      int64                        `json:"active_sources"`
	PausedSources      int64                        `json:"paused_sources"`
	HealthySources     int64                        `json:"healthy_sources"`
	DegradedSources    int64                        `json:"degraded_sources"`
	FailingSources     int64                        `json:"failing_sources"`
	StaleSources       int64                        `json:"stale_sources"`
	NeverRunSources    int64                        `json:"never_run_sources"`
	TotalRuns          int64                        `json:"total_runs"`
	SuccessfulRuns     int64                        `json:"successful_runs"`
	FailedRuns         int64                        `json:"failed_runs"`
	WarningRuns        int64                        `json:"warning_runs"`
	SuccessRate        float64                      `json:"success_rate"`
	RowsRead           int64                        `json:"rows_read"`
	InsertedCount      int64                        `json:"inserted_count"`
	UpdatedCount       int64                        `json:"updated_count"`
	SkippedCount       int64                        `json:"skipped_count"`
	ErrorCount         int64                        `json:"error_count"`
	LastRunAt          *time.Time                   `json:"last_run_at"`
	Sources            []OntologyFunnelSourceHealth `json:"sources"`
}

// OntologyFunnelSourceHealthResponse mirrors `struct OntologyFunnelSourceHealthResponse`.
type OntologyFunnelSourceHealthResponse struct {
	StaleAfterHours int64                      `json:"stale_after_hours"`
	SourceHealth    OntologyFunnelSourceHealth `json:"source_health"`
}
