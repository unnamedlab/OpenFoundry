// Package reconcile owns the IngestJob reconcile loop and the JobApplier
// abstraction used to materialise rendered Kubernetes resources.
//
// Two appliers are provided:
//
//   - LoggingApplier: the safe-default no-op used when the service is not
//     wired against a control plane.
//   - HTTPApplier (applier.go): the production applier that POSTs rendered
//     resources to a Kubernetes-backed control-plane HTTP shim.
package reconcile

import (
	"context"
	"log/slog"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/models"
)

// JobApplier materialises an IngestJob's rendered resources. Implementations
// must return the resource names (kafka connector, optional flink deployment)
// so the caller can persist them on the job row.
type JobApplier interface {
	Apply(ctx context.Context, job *models.IngestJob) (kafkaConnector, flinkDeployment string, err error)
}

// LoggingApplier is the default no-op JobApplier. It logs the request and
// returns empty resource names so the reconcile loop can run safely without
// a control-plane wired in.
type LoggingApplier struct {
	Logger *slog.Logger
}

// Apply records the request and returns no resource names.
func (a *LoggingApplier) Apply(ctx context.Context, job *models.IngestJob) (string, string, error) {
	if job == nil {
		return "", "", nil
	}
	logger := a.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.InfoContext(ctx, "logging applier no-op",
		slog.String("job_id", job.ID.String()),
		slog.String("name", job.Name),
		slog.String("namespace", job.Namespace),
	)
	return "", "", nil
}

// Reconciler orchestrates the IngestJob reconcile cadence. Apply implementations
// are pluggable via the Applier field; when nil, a LoggingApplier is used.
type Reconciler struct {
	Applier JobApplier
	Logger  *slog.Logger
}

// applier returns the configured applier or the safe-default LoggingApplier.
func (r *Reconciler) applier() JobApplier {
	if r != nil && r.Applier != nil {
		return r.Applier
	}
	return &LoggingApplier{Logger: r.logger()}
}

func (r *Reconciler) logger() *slog.Logger {
	if r != nil && r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

// Apply runs the configured applier against a single IngestJob. Exposed so
// callers (HTTP handlers, gRPC, tests) can drive a one-shot reconcile.
func (r *Reconciler) Apply(ctx context.Context, job *models.IngestJob) (string, string, error) {
	return r.applier().Apply(ctx, job)
}
