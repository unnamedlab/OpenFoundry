// Package queryrouter ports `services/lineage-service/src/query_router.rs`
// 1:1. Pure logic so the routing decision is unit-testable and shared
// across handlers + domain. Keeping the same constants, enum values
// and degradation rules guarantees the headers
// (`x-openfoundry-lineage-*`) and metric labels stay byte-equal across
// runtimes during cutover.
package queryrouter

import (
	"strings"
	"time"
)

// HotWindow is the threshold below which a query is "hot" — last 24 h.
const HotWindow = 24 * time.Hour

// HotWindowHours mirrors the Rust HOT_WINDOW_HOURS constant.
const HotWindowHours uint32 = 24

// TrinoReaderImplemented reflects whether this build has the Trino
// historical reader wired. The Rust copy is `false` until S5.6 lands;
// keeping the same default lets the degradation path stay observable.
const TrinoReaderImplemented = false

// QuerySource is where a lineage read should be sent.
type QuerySource string

const (
	SourceCassandra QuerySource = "cassandra"
	SourceTrino     QuerySource = "trino"
)

// AsMetricLabel returns the stable identifier emitted into metrics.
func (s QuerySource) AsMetricLabel() string {
	return string(s)
}

// QueryKind enumerates the logical lineage read shapes.
type QueryKind string

const (
	KindDatasetGraph   QueryKind = "dataset_graph"
	KindDatasetImpact  QueryKind = "dataset_impact"
	KindDatasetColumns QueryKind = "dataset_columns"
	KindFullGraph      QueryKind = "full_graph"
)

// AsScopeLabel returns the value used in the
// `x-openfoundry-lineage-scope` response header.
func (k QueryKind) AsScopeLabel() string { return string(k) }

// DefaultWindowHours mirrors `QueryKind::default_window_hours()`.
//
// Dataset-scoped reads stay on the hot path by default; the full graph
// defaults to historical because it typically exceeds the operational
// Cassandra working set (HOT_WINDOW_HOURS + 1).
func (k QueryKind) DefaultWindowHours() uint32 {
	switch k {
	case KindDatasetGraph, KindDatasetImpact, KindDatasetColumns:
		return HotWindowHours
	case KindFullGraph:
		return HotWindowHours + 1
	default:
		return HotWindowHours
	}
}

// QueryPlan is the concrete routing decision for a lineage API read.
type QueryPlan struct {
	Kind             QueryKind
	WindowHours      uint32
	RequestedSource  QuerySource
	SelectedSource   QuerySource
	Degraded         bool
}

// IsHistorical returns true when the request originally targeted Trino.
func (p QueryPlan) IsHistorical() bool { return p.RequestedSource == SourceTrino }

// Route decides Cassandra vs Trino given the **age of the oldest row**
// the query needs. Both arguments are caller-supplied to keep the
// function pure (no time.Now inside).
func Route(nowUnix, oldestNeededUnix int64) QuerySource {
	age := nowUnix - oldestNeededUnix
	if age < 0 {
		age = 0
	}
	if age <= int64(HotWindow.Seconds()) {
		return SourceCassandra
	}
	return SourceTrino
}

// RouteWindow takes a *requested window* in hours.
func RouteWindow(windowHours uint32) QuerySource {
	if uint64(windowHours)*3600 <= uint64(HotWindow.Seconds()) {
		return SourceCassandra
	}
	return SourceTrino
}

// TrinoEnabledFromEnv mirrors `trino_enabled_from_env`. Reads
// `LINEAGE_TRINO_ENABLED`; case-sensitive same as Rust.
func TrinoEnabledFromEnv(envValue *string) bool {
	if envValue == nil {
		return false
	}
	switch strings.TrimSpace(*envValue) {
	case "1", "true", "TRUE", "yes":
		return true
	default:
		return false
	}
}

// TrinoAvailableFromEnv ANDs the deployment flag with the build-time
// implementation flag.
func TrinoAvailableFromEnv(envValue *string) bool {
	return TrinoEnabledFromEnv(envValue) && TrinoReaderImplemented
}

// Plan builds the handler/domain-visible routing plan.
//
// When historical Trino is requested but unavailable, callers MUST
// serve the Cassandra read-model as a degraded fallback (never pivot
// to PostgreSQL lineage relation tables — those are archive-only).
func Plan(kind QueryKind, windowHours *uint32, historical, trinoAvailable bool) QueryPlan {
	resolvedWindow := kind.DefaultWindowHours()
	if windowHours != nil {
		resolvedWindow = *windowHours
	}
	if historical && resolvedWindow <= HotWindowHours {
		resolvedWindow = HotWindowHours + 1
	}

	requested := RouteWindow(resolvedWindow)
	selected := requested
	if requested == SourceTrino && !trinoAvailable {
		selected = SourceCassandra
	}

	return QueryPlan{
		Kind:            kind,
		WindowHours:     resolvedWindow,
		RequestedSource: requested,
		SelectedSource:  selected,
		Degraded:        requested != selected,
	}
}
