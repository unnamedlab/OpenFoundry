package storage

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// HistogramBucket is one value/frequency bucket maintained for an OSV2
// property histogram. Bucket labels use the same normalized scalar encoding as
// the property index so estimates can be reused by the OQL planner.
type HistogramBucket struct {
	Value string `json:"value"`
	Count uint64 `json:"count"`
}

// PropertyHistogram summarizes per-type cardinality for one property.
type PropertyHistogram struct {
	Tenant       TenantId          `json:"tenant"`
	TypeID       TypeId            `json:"type_id"`
	PropertyName string            `json:"property_name"`
	TotalRows    uint64            `json:"total_rows"`
	NullRows     uint64            `json:"null_rows"`
	Distinct     uint64            `json:"distinct"`
	Buckets      []HistogramBucket `json:"buckets"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// LinkFanoutDistribution summarizes per-link fan-out for join-order planning.
type LinkFanoutDistribution struct {
	Tenant      TenantId   `json:"tenant"`
	LinkType    LinkTypeId `json:"link_type"`
	Direction   string     `json:"direction"`
	SourceNodes uint64     `json:"source_nodes"`
	EdgeCount   uint64     `json:"edge_count"`
	P50         uint64     `json:"p50"`
	P95         uint64     `json:"p95"`
	Max         uint64     `json:"max"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// StatisticsProvider exposes OSV2.21 cardinality estimates to the OQL planner.
type StatisticsProvider interface {
	PropertyHistogram(ctx context.Context, tenant TenantId, typeID TypeId, propertyName string) (PropertyHistogram, bool, error)
	RefreshStatistics(ctx context.Context, tenant TenantId, typeID TypeId) error
}

// LinkStatisticsProvider exposes OSV2.21 link fan-out distributions.
type LinkStatisticsProvider interface {
	LinkFanout(ctx context.Context, tenant TenantId, linkType LinkTypeId, direction string) (LinkFanoutDistribution, bool, error)
	RefreshLinkStatistics(ctx context.Context, tenant TenantId, linkType LinkTypeId) error
}

// QueryPlanStep is one access-path step in an OQL EXPLAIN plan.
type QueryPlanStep struct {
	Name              string   `json:"name"`
	AccessPath        string   `json:"access_path"`
	IndexName         string   `json:"index_name,omitempty"`
	Predicate         string   `json:"predicate,omitempty"`
	EstimatedRows     uint64   `json:"estimated_rows"`
	EstimatedTimeMs   float64  `json:"estimated_time_ms"`
	RestrictedFilters []string `json:"restricted_filters,omitempty"`
}

// QueryPlan is the OSV2.22 planner explanation returned by EXPLAIN.
type QueryPlan struct {
	Mode            string          `json:"mode"`
	IndexChoice     string          `json:"index_choice"`
	EstimatedRows   uint64          `json:"estimated_rows"`
	EstimatedTimeMs float64         `json:"estimated_time_ms"`
	Steps           []QueryPlanStep `json:"steps"`
}

// QueryActuals are appended for EXPLAIN ANALYZE and per-query accounting.
type QueryActuals struct {
	RowsScanned  uint64        `json:"rows_scanned"`
	IndicesHit   []string      `json:"indices_hit"`
	RowsReturned uint64        `json:"rows_returned"`
	WallTime     time.Duration `json:"-"`
	WallTimeMs   float64       `json:"wall_time_ms"`
	RateLimited  bool          `json:"rate_limited,omitempty"`
	Warning      string        `json:"warning,omitempty"`
}

// QueryCostRecord is the OSV2.26 accounting envelope aggregated by resource
// management views.
type QueryCostRecord struct {
	Tenant       TenantId      `json:"tenant"`
	ProjectID    string        `json:"project_id,omitempty"`
	CallerID     string        `json:"caller_id,omitempty"`
	TypeID       TypeId        `json:"type_id"`
	RowsScanned  uint64        `json:"rows_scanned"`
	IndicesHit   []string      `json:"indices_hit"`
	RowsReturned uint64        `json:"rows_returned"`
	WallTime     time.Duration `json:"-"`
	WallTimeMs   float64       `json:"wall_time_ms"`
	RecordedAt   time.Time     `json:"recorded_at"`
}

// QueryCostRecorder stores query cost records for OSV2.26.
type QueryCostRecorder interface {
	RecordQueryCost(ctx context.Context, record QueryCostRecord) error
	QueryCostSummary(ctx context.Context, tenant TenantId, projectID string) (QueryCostSummary, error)
}

// QueryCostSummary is an aggregate for cost insights.
type QueryCostSummary struct {
	Tenant       TenantId `json:"tenant"`
	ProjectID    string   `json:"project_id,omitempty"`
	QueryCount   uint64   `json:"query_count"`
	RowsScanned  uint64   `json:"rows_scanned"`
	RowsReturned uint64   `json:"rows_returned"`
	WallTimeMs   float64  `json:"wall_time_ms"`
	IndicesHit   []string `json:"indices_hit"`
}

// QueryBudget describes a per-caller or per-project budget window.
type QueryBudget struct {
	Limit        uint64        `json:"limit"`
	Used         uint64        `json:"used"`
	Window       time.Duration `json:"-"`
	WindowMs     int64         `json:"window_ms"`
	ResetsAt     time.Time     `json:"resets_at"`
	SoftWarning  bool          `json:"soft_warning"`
	RetryAfter   time.Duration `json:"-"`
	RetryAfterMs int64         `json:"retry_after_ms,omitempty"`
}

// QueryBudgetEnforcer applies OSV2.27 per-caller / per-project budgets.
type QueryBudgetEnforcer interface {
	ReserveQueryBudget(ctx context.Context, tenant TenantId, projectID, callerID string, units uint64) (QueryBudget, error)
}

// MaterializedAggregate declares an OSV2.23 incrementally maintained aggregate.
type MaterializedAggregate struct {
	Name         string    `json:"name"`
	Tenant       TenantId  `json:"tenant"`
	TypeID       TypeId    `json:"type_id"`
	Function     string    `json:"function"`
	PropertyName string    `json:"property_name,omitempty"`
	GroupBy      string    `json:"group_by,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// MaterializedAggregateResult is read when the planner rewrites a compatible query.
type MaterializedAggregateResult struct {
	Aggregate MaterializedAggregate `json:"aggregate"`
	Value     any                   `json:"value"`
	Groups    map[string]any        `json:"groups,omitempty"`
	Count     uint64                `json:"count"`
}

// MaterializedAggregateStore declares and reads materialized aggregates.
type MaterializedAggregateStore interface {
	DeclareMaterializedAggregate(ctx context.Context, aggregate MaterializedAggregate) error
	ReadMaterializedAggregate(ctx context.Context, tenant TenantId, typeID TypeId, function, propertyName, groupBy string) (MaterializedAggregateResult, bool, error)
}

func normalizeHistogramValue(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.ToLower(strings.TrimSpace(t))
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		b, _ := json.Marshal(t)
		return strings.ToLower(strings.TrimSpace(string(b)))
	}
}

func percentileUint64(values []uint64, percentile float64) uint64 {
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	idx := int(float64(len(values)-1) * percentile)
	if idx < 0 {
		idx = 0
	}
	if idx >= len(values) {
		idx = len(values) - 1
	}
	return values[idx]
}
