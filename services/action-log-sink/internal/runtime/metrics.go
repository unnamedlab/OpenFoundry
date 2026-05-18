// Package runtime is the action-log-sink Kafka → Writer batching loop.
package runtime

import "github.com/prometheus/client_golang/prometheus"

// Metric names — pinned. Renaming breaks dashboards and alerts.
const (
	MetricLagSeconds   = "action_log_sink_lag_seconds"
	MetricRecordsTotal = "action_log_sink_records_total"
	MetricBatchSize    = "action_log_sink_batch_size_records"
	MetricCommitsTotal = "action_log_sink_commits_total"
)

// Outcome label values for action_log_sink_commits_total.
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
	OutcomePoison  = "poison"
)

// Metrics bundles the collectors emitted by the runtime. All metrics
// are unlabelled by table (single target table: action_log); the
// commits counter carries an `outcome` label only.
type Metrics struct {
	Registry     *prometheus.Registry
	LagSeconds   prometheus.Histogram
	RecordsTotal prometheus.Counter
	BatchSize    prometheus.Histogram
	CommitsTotal *prometheus.CounterVec
}

// NewMetrics returns a fresh Registry plus the four sink collectors.
// Buckets mirror the ai-sink / audit-sink shape so dashboards can
// overlay the three sinks side-by-side.
func NewMetrics() *Metrics {
	reg := prometheus.NewRegistry()

	lag := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    MetricLagSeconds,
		Help:    "Seconds between action applied_at_ms and successful Iceberg append.",
		Buckets: []float64{0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0, 120.0, 300.0},
	})
	records := prometheus.NewCounter(prometheus.CounterOpts{
		Name: MetricRecordsTotal,
		Help: "Total ontology-action records appended to action_log.",
	})
	batch := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    MetricBatchSize,
		Help:    "Records per successful Iceberg append.",
		Buckets: []float64{1, 10, 100, 1_000, 10_000, 50_000, 100_000},
	})
	commits := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: MetricCommitsTotal,
		Help: "Iceberg append attempts by outcome (success | failure | poison).",
	}, []string{"outcome"})

	reg.MustRegister(lag, records, batch, commits)
	return &Metrics{
		Registry:     reg,
		LagSeconds:   lag,
		RecordsTotal: records,
		BatchSize:    batch,
		CommitsTotal: commits,
	}
}
