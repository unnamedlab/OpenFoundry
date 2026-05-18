// Package server hosts the action-log-sink HTTP surface — `/healthz`,
// `/metrics`, and the read/write `/api/v1/action-log/*` API backing the
// query side of the action_log_events hot store.
//
// Shape mirrors services/audit-sink/internal/server so the two sinks
// stay uniform behind kubelet probes, Prometheus scrape, and any future
// shared client SDK.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/openfoundry/openfoundry-go/libs/core-models/health"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/runtime"
)

// Server bundles the chi router + http.Server.
type Server struct {
	srv    *http.Server
	Router chi.Router
}

// New wires:
//
//	GET  /healthz
//	GET  /readyz
//	GET  /metrics
//	GET  /api/v1/action-log/events
//	GET  /api/v1/action-log/events/{event_id}
//	GET  /api/v1/action-log/events/export   (NDJSON stream)
//	POST /api/v1/action-log/events          (write-through; see handlers.RecordEvent)
//
// `h` may be nil — in Iceberg-only deployments (no Postgres pool) the
// API routes are skipped and only /healthz, /readyz, /metrics are served.
func New(addr, serviceName, version string, m *runtime.Metrics, h *handlers.Handlers) *Server {
	r := chi.NewRouter()
	r.Get("/healthz", healthHandler(serviceName, version))
	r.Get("/readyz", healthHandler(serviceName, version))
	r.Handle("/metrics", promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{Registry: m.Registry}))
	if h != nil {
		r.Route("/api/v1/action-log", func(r chi.Router) {
			r.Get("/events", h.QueryEvents)
			r.Get("/events/export", h.ExportEvents)
			r.Get("/events/{event_id}", h.GetEvent)
			r.Post("/events", h.RecordEvent)
		})
	}
	return &Server{
		srv: &http.Server{
			Addr:              addr,
			Handler:           r,
			ReadTimeout:       10 * time.Second,
			ReadHeaderTimeout: 2 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       120 * time.Second,
		},
		Router: r,
	}
}

// Run blocks until ctx is done or the listener returns.
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func healthHandler(serviceName, version string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(serviceName, version))
	}
}
