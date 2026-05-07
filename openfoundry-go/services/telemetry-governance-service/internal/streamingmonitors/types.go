// Package streamingmonitors implements the Foundry-parity Stream
// Monitoring surface (Bloque P4).
//
// Three concepts:
//   - MonitoringView: groups rules under a project_rid.
//   - MonitorRule: typed alert rule (resource_type × monitor_kind ×
//     comparator × threshold) over a time window.
//   - MonitorEvaluation: audit trail of scheduler ticks.
//
// The evaluator (scheduler-side) lives outside this package and is not
// part of the HTTP surface — it persists evaluations directly. This
// package owns the wire format and the CRUD endpoints consumed by the
// Data Health UI.
package streamingmonitors

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

// ResourceType discriminates which streaming resource a rule watches.
type ResourceType string

const (
	ResourceStreamingDataset       ResourceType = "STREAMING_DATASET"
	ResourceStreamingPipeline      ResourceType = "STREAMING_PIPELINE"
	ResourceTimeSeriesSync         ResourceType = "TIME_SERIES_SYNC"
	ResourceGeotemporalObservations ResourceType = "GEOTEMPORAL_OBSERVATIONS"
)

// IsValid mirrors the Rust `from_str` allow-list.
func (r ResourceType) IsValid() bool {
	switch r {
	case ResourceStreamingDataset, ResourceStreamingPipeline,
		ResourceTimeSeriesSync, ResourceGeotemporalObservations:
		return true
	}
	return false
}

// MonitorKind picks the metric family the evaluator queries.
type MonitorKind string

const (
	KindIngestRecords                 MonitorKind = "INGEST_RECORDS"
	KindOutputRecords                 MonitorKind = "OUTPUT_RECORDS"
	KindCheckpointLiveness            MonitorKind = "CHECKPOINT_LIVENESS"
	KindLastCheckpointDuration        MonitorKind = "LAST_CHECKPOINT_DURATION"
	KindCheckpointTriggerFailures     MonitorKind = "CHECKPOINT_TRIGGER_FAILURES"
	KindConsecutiveCheckpointFailures MonitorKind = "CONSECUTIVE_CHECKPOINT_FAILURES"
	KindTotalLag                      MonitorKind = "TOTAL_LAG"
	KindTotalThroughput               MonitorKind = "TOTAL_THROUGHPUT"
	KindUtilization                   MonitorKind = "UTILIZATION"
	KindPointsWrittenToTS             MonitorKind = "POINTS_WRITTEN_TO_TS"
	KindGeotemporalObsSent            MonitorKind = "GEOTEMPORAL_OBS_SENT"
)

func (k MonitorKind) IsValid() bool {
	switch k {
	case KindIngestRecords, KindOutputRecords, KindCheckpointLiveness,
		KindLastCheckpointDuration, KindCheckpointTriggerFailures,
		KindConsecutiveCheckpointFailures, KindTotalLag, KindTotalThroughput,
		KindUtilization, KindPointsWrittenToTS, KindGeotemporalObsSent:
		return true
	}
	return false
}

// Comparator is the threshold operator.
type Comparator string

const (
	CmpLT  Comparator = "LT"
	CmpLTE Comparator = "LTE"
	CmpGT  Comparator = "GT"
	CmpGTE Comparator = "GTE"
	CmpEQ  Comparator = "EQ"
)

func (c Comparator) IsValid() bool {
	switch c {
	case CmpLT, CmpLTE, CmpGT, CmpGTE, CmpEQ:
		return true
	}
	return false
}

// Evaluate reports whether the observed value crosses the threshold.
//
// EQ uses a small relative tolerance (matches Rust f64-EPSILON guard)
// so floating-point noise doesn't mask matches.
func (c Comparator) Evaluate(observed, threshold float64) bool {
	switch c {
	case CmpLT:
		return observed < threshold
	case CmpLTE:
		return observed <= threshold
	case CmpGT:
		return observed > threshold
	case CmpGTE:
		return observed >= threshold
	case CmpEQ:
		eps := math.Nextafter(1, 2) - 1
		tol := eps
		if abs := math.Abs(threshold) * 1e-9; abs > tol {
			tol = abs
		}
		return math.Abs(observed-threshold) <= tol
	}
	return false
}

// Severity tags how loudly the firing rule should be surfaced.
type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityWarn     Severity = "WARN"
	SeverityCritical Severity = "CRITICAL"
)

func (s Severity) IsValid() bool {
	switch s {
	case SeverityInfo, SeverityWarn, SeverityCritical:
		return true
	}
	return false
}

// MonitoringView mirrors the Rust struct.
type MonitoringView struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	ProjectRID  string    `json:"project_rid"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateMonitoringViewRequest is the body of POST /v1/monitoring-views.
type CreateMonitoringViewRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	ProjectRID  string  `json:"project_rid"`
}

// MonitorRule mirrors the Rust struct (typed enums on the wire).
type MonitorRule struct {
	ID             uuid.UUID    `json:"id"`
	ViewID         uuid.UUID    `json:"view_id"`
	Name           string       `json:"name"`
	ResourceType   ResourceType `json:"resource_type"`
	ResourceRID    string       `json:"resource_rid"`
	MonitorKind    MonitorKind  `json:"monitor_kind"`
	WindowSeconds  int32        `json:"window_seconds"`
	Comparator     Comparator   `json:"comparator"`
	Threshold      float64      `json:"threshold"`
	Severity       Severity     `json:"severity"`
	Enabled        bool         `json:"enabled"`
	CreatedBy      string       `json:"created_by"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

// CreateMonitorRuleRequest is the body of POST /v1/monitoring-views/{id}/rules.
type CreateMonitorRuleRequest struct {
	ViewID        uuid.UUID    `json:"view_id"`
	Name          *string      `json:"name,omitempty"`
	ResourceType  ResourceType `json:"resource_type"`
	ResourceRID   string       `json:"resource_rid"`
	MonitorKind   MonitorKind  `json:"monitor_kind"`
	WindowSeconds int32        `json:"window_seconds"`
	Comparator    Comparator   `json:"comparator"`
	Threshold     float64      `json:"threshold"`
	Severity      Severity     `json:"severity,omitempty"`
}

// Validate mirrors the Rust validate() — window 60..=86400, non-empty
// resource_rid, finite threshold, plus typed enum validity.
func (r *CreateMonitorRuleRequest) Validate() error {
	if !r.ResourceType.IsValid() {
		return fmt.Errorf("invalid resource_type: %q", r.ResourceType)
	}
	if !r.MonitorKind.IsValid() {
		return fmt.Errorf("invalid monitor_kind: %q", r.MonitorKind)
	}
	if !r.Comparator.IsValid() {
		return fmt.Errorf("invalid comparator: %q", r.Comparator)
	}
	if r.Severity != "" && !r.Severity.IsValid() {
		return fmt.Errorf("invalid severity: %q", r.Severity)
	}
	if r.WindowSeconds < 60 || r.WindowSeconds > 86_400 {
		return fmt.Errorf("window_seconds must be between 60 and 86400 (got %d)", r.WindowSeconds)
	}
	if trimEmpty(r.ResourceRID) {
		return errors.New("resource_rid must not be empty")
	}
	if math.IsNaN(r.Threshold) || math.IsInf(r.Threshold, 0) {
		return errors.New("threshold must be a finite number")
	}
	return nil
}

// PatchRuleRequest is the body of PATCH /v1/monitor-rules/{id}.
//
// Every field is optional; nil preserves the current value.
type PatchRuleRequest struct {
	Name          *string     `json:"name,omitempty"`
	WindowSeconds *int32      `json:"window_seconds,omitempty"`
	Comparator    *Comparator `json:"comparator,omitempty"`
	Threshold     *float64    `json:"threshold,omitempty"`
	Severity      *Severity   `json:"severity,omitempty"`
	Enabled       *bool       `json:"enabled,omitempty"`
}

// MonitorEvaluation is the audit row written by the evaluator.
type MonitorEvaluation struct {
	ID            uuid.UUID  `json:"id"`
	RuleID        uuid.UUID  `json:"rule_id"`
	EvaluatedAt   time.Time  `json:"evaluated_at"`
	ObservedValue float64    `json:"observed_value"`
	Fired         bool       `json:"fired"`
	AlertID       *uuid.UUID `json:"alert_id"`
}

// DataEnvelope is the canonical streaming-monitor list envelope.
//
// Note: this surface uses {"data": [...]} (NOT {"items": [...]}) for
// historical wire-compat — the Rust impl predates the {"items"} shape
// used elsewhere in the workspace. Tests pin this so a future refactor
// can't accidentally swap envelopes.
type DataEnvelope[T any] struct {
	Data []T `json:"data"`
}

func trimEmpty(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

// ensure encoding/json import is used — types reference it via tags only.
var _ = json.Marshal
