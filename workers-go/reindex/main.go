// reindex worker — Temporal Go SDK.
//
// Mirrors the structure of `workers-go/workflow-automation/main.go`
// (S2.7). One binary, one task queue (`openfoundry.reindex`), the
// workflow `OntologyReindex` defined in `workflows/`. Activities
// are stubs until the Rust side wires the Cassandra scanner +
// Kafka publisher.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	sdkclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/open-foundry/open-foundry/workers-go/reindex/activities"
	"github.com/open-foundry/open-foundry/workers-go/reindex/internal/contract"
	"github.com/open-foundry/open-foundry/workers-go/reindex/workflows"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLogLevel()}))
	slog.SetDefault(logger)

	hostPort := getenv("TEMPORAL_HOST_PORT", "127.0.0.1:7233")
	namespace := getenv("TEMPORAL_NAMESPACE", "default")

	c, err := sdkclient.Dial(sdkclient.Options{
		HostPort:  hostPort,
		Namespace: namespace,
	})
	if err != nil {
		logger.Error("temporal dial failed", "err", err, "host_port", hostPort)
		os.Exit(1)
	}
	defer c.Close()

	w := worker.New(c, contract.TaskQueue, worker.Options{})

	w.RegisterWorkflow(workflows.OntologyReindex)
	w.RegisterActivity(&activities.Activities{})

	go serveMetrics(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := w.Run(ctx); err != nil {
		logger.Error("worker run failed", "err", err)
		os.Exit(1)
	}
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func parseLogLevel() slog.Level {
	switch strings.ToLower(os.Getenv("LOG_LEVEL")) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func serveMetrics(logger *slog.Logger) {
	addr := getenv("METRICS_ADDR", ":9095")
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("metrics server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("metrics server failed", "err", err)
	}
}
