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

	"github.com/open-foundry/open-foundry/workers-go/automation-ops/activities"
	"github.com/open-foundry/open-foundry/workers-go/automation-ops/internal/contract"
	"github.com/open-foundry/open-foundry/workers-go/automation-ops/workflows"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLogLevel()}))
	slog.SetDefault(logger)

	hostPort := firstEnv("127.0.0.1:7233", "TEMPORAL_ADDRESS", "TEMPORAL_HOST_PORT")
	namespace := getenv("TEMPORAL_NAMESPACE", "default")
	taskQueue := getenv("TEMPORAL_TASK_QUEUE", contract.TaskQueue)

	c, err := sdkclient.Dial(sdkclient.Options{
		HostPort:  hostPort,
		Namespace: namespace,
	})
	if err != nil {
		logger.Error("temporal dial failed", "err", err)
		os.Exit(1)
	}
	defer c.Close()

	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(workflows.AutomationOpsTask)
	w.RegisterActivity(activities.New())

	go serveMetrics(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger.Info("worker starting", "task_queue", taskQueue, "namespace", namespace)
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
		_, _ = w.Write([]byte("# automation_ops_worker metrics exported by process supervisor\n"))
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

func firstEnv(def string, keys ...string) string {
	for _, key := range keys {
		if v := os.Getenv(key); v != "" {
			return v
		}
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
