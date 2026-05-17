// Package server exposes the /healthz + /metrics surface so kubelet
// probes and Prometheus can target the indexer pod alongside the
// long-running scan. Matches the lightweight shape used by
// services/pipeline-runner/internal/server.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics carries the Prometheus collectors the runner increments
// while it indexes. The receiver methods satisfy the
// runner.Metrics interface without dragging Prometheus into runner
// (so tests can inject a no-op).
type Metrics struct {
	Rows     *prometheus.CounterVec
	Batches  prometheus.Counter
	Duration prometheus.Histogram
}

// NewMetrics registers the collectors on `reg` and returns the
// handle. Panics on duplicate registration so misconfiguration
// surfaces at boot.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		Rows: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "iceberg_object_indexer_rows_total",
			Help: "Rows processed by the iceberg-object-indexer, labelled by outcome.",
		}, []string{"outcome"}),
		Batches: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "iceberg_object_indexer_batches_total",
			Help: "Arrow record batches received from the iceberg source.",
		}),
		Duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "iceberg_object_indexer_duration_seconds",
			Help:    "End-to-end duration of an indexing run in seconds.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 12),
		}),
	}
	reg.MustRegister(m.Rows, m.Batches, m.Duration)
	return m
}

func (m *Metrics) RecordRow(outcome string)       { m.Rows.WithLabelValues(outcome).Inc() }
func (m *Metrics) RecordBatch(_ int)              { m.Batches.Inc() }
func (m *Metrics) RecordDuration(d time.Duration) { m.Duration.Observe(d.Seconds()) }

// Server wraps the http.Server lifecycle for /healthz + /metrics.
type Server struct {
	srv *http.Server
}

// New returns a Server bound to `addr` exposing /healthz and /metrics.
// The /metrics endpoint scrapes the supplied registry only — the
// runner does not touch the default Prometheus registry to keep
// tests hermetic.
func New(addr, serviceName, version string, reg *prometheus.Registry) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"service": serviceName,
			"version": version,
		})
	})
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	return &Server{srv: &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}}
}

// Addr returns the listener address the server was configured with.
func (s *Server) Addr() string { return s.srv.Addr }

// Run blocks until ctx is done or the listener returns. On ctx done
// it performs a graceful shutdown with a 5s budget.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
