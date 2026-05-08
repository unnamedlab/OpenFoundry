// Package metrics declares the Prometheus families specific to
// media-sets-service. Mirrors libs/services/media-sets-service/src/metrics.rs:
// names, labels and help text are byte-for-byte identical so dashboards
// and the Prometheus rules in
// infra/k8s/platform/observability/prometheus-rules/media-sets.yaml
// keep working unchanged.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/openfoundry/openfoundry-go/libs/observability"
)

// Metrics holds the families used by access-pattern + media-item paths.
// Created once at boot, registered on observability.Metrics.Registry,
// then threaded through to handlers/services.
type Metrics struct {
	// MediaComputeSecondsTotal is the per-(transformation, schema)
	// counter of compute-seconds consumed by access-pattern work.
	// Base counter for the tenant cost model; rates come from
	// libs/observability/costmodel.
	MediaComputeSecondsTotal *prometheus.CounterVec
	// MediaActiveTransactions is the per-media-set gauge of currently
	// OPEN transactions. The transactions service bumps it on open
	// and decrements it on close (commit/abort).
	MediaActiveTransactions *prometheus.GaugeVec
	// MediaRetentionPurgesTotal is the global counter of items the
	// retention reaper soft-deletes. Anomalous spikes are the
	// canonical "retention reduced accidentally" signal.
	MediaRetentionPurgesTotal prometheus.Counter
}

// New registers the families on the given observability.Metrics
// registry and returns the typed handle. Pre-evaluates each family so
// /metrics surfaces the prefix even before the first invocation.
func New(o *observability.Metrics) *Metrics {
	m := &Metrics{
		MediaComputeSecondsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "media_compute_seconds_total",
				Help: "Compute seconds consumed by media access-pattern work, per transformation and schema. Base counter for the tenant cost model.",
			},
			[]string{"transformation", "schema"},
		),
		MediaActiveTransactions: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "media_active_transactions",
				Help: "Currently OPEN media-set transactions, by media set RID.",
			},
			[]string{"media_set"},
		),
		MediaRetentionPurgesTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "media_retention_purges_total",
				Help: "Total number of media items soft-deleted by the retention reaper.",
			},
		),
	}
	o.Register(m.MediaComputeSecondsTotal)
	o.Register(m.MediaActiveTransactions)
	o.Register(m.MediaRetentionPurgesTotal)
	return m
}
