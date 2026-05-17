// Package server hosts the pipeline-runner HTTP surface — /healthz and
// /metrics — so kubelet probes and Prometheus can target the Spark
// driver Pod alongside the spark-submit subprocess.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/openfoundry/openfoundry-go/libs/core-models/health"
)

// Server wraps the http.Server lifecycle.
type Server struct{ srv *http.Server }

// New wires GET /healthz + GET /metrics on `addr`. /metrics uses the
// default Prometheus registry — the runner registers no custom
// collectors yet, so Go runtime + process metrics are the surface.
func New(addr, serviceName, version string) *Server {
	return &Server{srv: &http.Server{
		Addr:              addr,
		Handler:           Handler(serviceName, version),
		ReadHeaderTimeout: 5 * time.Second,
	}}
}

// Handler returns the bare mux New wraps. Exported so tests can
// exercise /healthz without binding a port.
func Handler(serviceName, version string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(health.OK(serviceName, version))
	})
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}

// Addr returns the listener address the server was configured with.
func (s *Server) Addr() string { return s.srv.Addr }

// Run blocks until ctx is done or the listener returns. On ctx done it
// performs a graceful shutdown with a 5s budget.
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
