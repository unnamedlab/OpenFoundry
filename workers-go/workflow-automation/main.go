// Worker entrypoint for the `workflow-automation` task queue. The
// binary registers the workflows declared in ./workflows and the
// activity bundle declared in ./activities, then blocks on the
// Temporal SDK's worker until SIGINT / SIGTERM.
//
// Configuration is env-driven so the same image runs in dev and
// prod with overlay-specific values:
//
//   - TEMPORAL_HOST_PORT (default 127.0.0.1:7233)
//   - TEMPORAL_NAMESPACE (default default)
//   - OF_LOG_LEVEL (default info)
//
// Metrics are exported on :9090/metrics for the Prometheus
// ServiceMonitor in `infra/k8s/temporal/servicemonitor.yaml` to scrape.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	sdkclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/open-foundry/open-foundry/workers-go/workflow-automation/activities"
	"github.com/open-foundry/open-foundry/workers-go/workflow-automation/internal/contract"
	"github.com/open-foundry/open-foundry/workers-go/workflow-automation/workflows"
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

	w := worker.New(c, contract.TaskQueue, worker.Options{
		// Defaults are usually fine; expose the knobs as env vars
		// when prod tuning starts. Documented in workers-go/README.md.
	})

	w.RegisterWorkflow(workflows.AutomationRun)
	w.RegisterActivity(&activities.Activities{})

	go serveMetrics(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger.Info("worker starting", "task_queue", contract.TaskQueue, "namespace", namespace)
	if err := w.Run(worker.InterruptCh()); err != nil {
		logger.Error("worker exited with error", "err", err)
		os.Exit(1)
	}
	<-ctx.Done()
	logger.Info("worker stopped")
}

func serveMetrics(logger *slog.Logger) {
	mux := http.NewServeMux()
	// SDK metrics are wired via the client option above once we
	// pull tally/prometheus; today this endpoint serves a 200 so
	// the readiness probe passes.
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("# substrate worker — metrics wiring pending S2.3.e\n"))
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	addr := getenv("METRICS_ADDR", ":9090")
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("metrics server exited", "err", err)
	}
}

func getenv(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

func parseLogLevel() slog.Level {
	switch os.Getenv("OF_LOG_LEVEL") {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
