// Package reconcile owns the IngestJob reconcile loop and the JobApplier
// abstraction used to materialise rendered Kubernetes resources.
//
// Two appliers are provided:
//
//   - LoggingApplier: an explicit dev/test no-op selected only by startup
//     configuration when no control plane should be contacted.
//   - HTTPApplier (applier.go): the production applier that POSTs rendered
//     resources to a Kubernetes-backed control-plane HTTP shim.
package reconcile

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/models"
)

// JobApplier materialises an IngestJob's rendered resources. Implementations
// must return the resource names (kafka connector, optional flink deployment)
// so the caller can persist them on the job row.
type JobApplier interface {
	Apply(ctx context.Context, job *models.IngestJob) (kafkaConnector, flinkDeployment string, err error)
}

// LoggingApplier is an explicit dev/test no-op JobApplier. It logs the request
// and returns empty resource names without materialising control-plane resources.
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
	logger.WarnContext(ctx, "INGESTION RECONCILER NO-OP APPLY; CONTROL-PLANE RESOURCES WERE NOT MATERIALIZED",
		slog.String("job_id", job.ID.String()),
		slog.String("name", job.Name),
		slog.String("namespace", job.Namespace),
	)
	return "", "", nil
}

// Reconciler orchestrates the IngestJob reconcile cadence. Apply implementations
// are pluggable via the Applier field; startup must inject one explicitly.
type Reconciler struct {
	Applier JobApplier
	Logger  *slog.Logger
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
	if r == nil || r.Applier == nil {
		return "", "", fmt.Errorf("ingest job reconciler has no applier configured")
	}
	return r.Applier.Apply(ctx, job)
}
