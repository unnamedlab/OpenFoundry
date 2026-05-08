// Retention runner — periodically polls the active policies + their
// targets, marks the files for retirement, and physically deletes
// after the per-policy grace period elapses. Mirrors
// `services/audit-compliance-service/src/lineage_deletion/domain/retention_runner.rs`
// 1:1.

package lineagedeletion

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// RetentionDeps bundles every side-effect the runner needs. The
// in-memory test fixture lives next to the unit tests; the
// production deps wiring lives in main.go (HTTP + storage backends).
type RetentionDeps interface {
	ListActivePolicies(ctx context.Context) ([]models.RetentionPolicySnapshot, error)
	EnumerateTargets(ctx context.Context, policy *models.RetentionPolicySnapshot) ([]models.RetentionTarget, error)
	OpenDeleteAndRetire(ctx context.Context, target *models.RetentionTarget) (uuid.UUID, error)
	PhysicalDelete(ctx context.Context, fileRefs []string) error
	PublishApplied(ctx context.Context, event *models.RetentionAppliedEvent) error
	Now() time.Time
	DeletionMarkedAt(ctx context.Context, datasetRid string, transactionID uuid.UUID) (*time.Time, error)
}

// ApplyPolicy ports `domain::retention_runner::apply_policy` 1:1.
func ApplyPolicy(ctx context.Context, deps RetentionDeps, policy *models.RetentionPolicySnapshot) (models.RetentionApplied, error) {
	targets, err := deps.EnumerateTargets(ctx, policy)
	if err != nil {
		return models.RetentionApplied{}, err
	}
	applied := models.RetentionApplied{PolicyID: policy.ID}
	for i := range targets {
		target := targets[i]
		applied.TargetsProcessed++
		applied.FilesMarked += len(target.FileRefs)
		applied.BytesFreed += target.Bytes

		// Step 1+2: open the DELETE txn + retire the files.
		if _, err := deps.OpenDeleteAndRetire(ctx, &target); err != nil {
			return models.RetentionApplied{}, err
		}
		// Step 3: physical deletion gated by the grace period.
		markedAt, err := deps.DeletionMarkedAt(ctx, target.DatasetRid, target.TransactionID)
		if err != nil {
			return models.RetentionApplied{}, err
		}
		now := deps.Now()
		grace := time.Duration(policy.GracePeriodMinutes) * time.Minute
		if grace < 0 {
			grace = 0
		}
		physicallyDeleted := false
		if markedAt != nil && now.Sub(*markedAt) >= grace {
			if err := deps.PhysicalDelete(ctx, target.FileRefs); err != nil {
				return models.RetentionApplied{}, err
			}
			applied.PhysicalDeletes++
			physicallyDeleted = true
		} else {
			applied.PhysicalDeleteSkippedGrace++
		}
		// Step 4: publish + audit-trail emission per target.
		event := &models.RetentionAppliedEvent{
			PolicyID:          policy.ID,
			PolicyName:        policy.Name,
			DatasetRid:        target.DatasetRid,
			TransactionID:     target.TransactionID,
			FilesCount:        len(target.FileRefs),
			BytesFreed:        target.Bytes,
			PhysicallyDeleted: physicallyDeleted,
			OccurredAt:        now,
		}
		if err := deps.PublishApplied(ctx, event); err != nil {
			return models.RetentionApplied{}, err
		}
		EmitRetentionDelete(&RetentionAuditRecord{
			PolicyID:          event.PolicyID,
			DatasetRid:        event.DatasetRid,
			TransactionID:     event.TransactionID,
			FilesCount:        event.FilesCount,
			BytesFreed:        event.BytesFreed,
			PhysicallyDeleted: event.PhysicallyDeleted,
		})
	}
	return applied, nil
}

// RunOnce ports `domain::retention_runner::run_once`. Errors per
// policy are logged + skipped so a bad row doesn't tank the loop.
func RunOnce(ctx context.Context, deps RetentionDeps) ([]models.RetentionApplied, error) {
	policies, err := deps.ListActivePolicies(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]models.RetentionApplied, 0, len(policies))
	for i := range policies {
		applied, err := ApplyPolicy(ctx, deps, &policies[i])
		if err != nil {
			slog.Error("retention policy run failed",
				slog.String("policy_id", policies[i].ID.String()),
				slog.String("error", err.Error()))
			continue
		}
		out = append(out, applied)
	}
	return out, nil
}

// RunLoop ports `domain::retention_runner::run_loop`. Zero interval
// disables the loop (used by tests / one-shot CLI runs).
func RunLoop(ctx context.Context, deps RetentionDeps, interval time.Duration) {
	if interval <= 0 {
		slog.Info("retention runner disabled (interval = 0)")
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := RunOnce(ctx, deps); err != nil {
				slog.Error("retention tick failed", slog.String("error", err.Error()))
			}
		}
	}
}

// SystemTimeDeps is a small mixin: `time.Now` for production,
// override-able for tests via embedding.
type SystemTimeDeps struct{}

// Now satisfies the RetentionDeps interface for callers that don't
// need a custom clock.
func (SystemTimeDeps) Now() time.Time { return time.Now().UTC() }

// Defence-in-depth: ensures the package keeps importing fmt so future
// helpers can format error chains without re-pulling the dep.
var _ = fmt.Errorf
