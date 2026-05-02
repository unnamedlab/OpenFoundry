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

	"github.com/open-foundry/open-foundry/workers-go/pipeline/activities"
	"github.com/open-foundry/open-foundry/workers-go/pipeline/internal/contract"
	"github.com/open-foundry/open-foundry/workers-go/pipeline/workflows"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLogLevel()}))
	slog.SetDefault(logger)

	c, err := sdkclient.Dial(sdkclient.Options{
		HostPort:  getenv("TEMPORAL_HOST_PORT", "127.0.0.1:7233"),
		Namespace: getenv("TEMPORAL_NAMESPACE", "default"),
	})
	if err != nil {
		logger.Error("temporal dial failed", "err", err)
		os.Exit(1)
	}
	defer c.Close()

	w := worker.New(c, contract.TaskQueue, worker.Options{})
	w.RegisterWorkflow(workflows.PipelineRun)
	w.RegisterActivity(activities.New())

	go serveMetrics(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger.Info("worker starting", "task_queue", contract.TaskQueue)
	if err := w.Run(worker.InterruptCh()); err != nil {
		logger.Error("worker exited with error", "err", err)
		os.Exit(1)
	}
	<-ctx.Done()
}

func serveMetrics(logger *slog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("# substrate worker — metrics wiring pending\n"))
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	if err := http.ListenAndServe(getenv("METRICS_ADDR", ":9090"), mux); err != nil {
		logger.Error("metrics server exited", "err", err)
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
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
