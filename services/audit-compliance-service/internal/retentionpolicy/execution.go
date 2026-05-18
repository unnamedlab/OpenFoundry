package retentionpolicy

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

const defaultRecoveryWindowDays int64 = 7

func BuildExecutionRun(preview models.RetentionPreviewResponse, request *models.RunRetentionExecutionRequest, actor string, orgID *uuid.UUID) models.RetentionExecutionRun {
	now := time.Now().UTC()
	recoveryDays := request.RecoveryWindowDays
	if recoveryDays <= 0 {
		recoveryDays = defaultRecoveryWindowDays
	}
	deadline := now.AddDate(0, 0, int(recoveryDays))
	run := models.RetentionExecutionRun{
		ID:                  uuid.New(),
		OrgID:               orgID,
		DatasetRid:          preview.DatasetRid,
		Status:              "completed",
		DryRun:              request.DryRun,
		RecoveryWindowDays:  recoveryDays,
		RemediationDeadline: &deadline,
		IrreversibleAfter:   &deadline,
		Warnings: append([]string{
			"Retention execution uses mark-and-sweep: marked transactions may be swept after the recovery window and are then irreversible.",
		}, preview.Warnings...),
		Items:       []models.RetentionExecutionItem{},
		CreatedBy:   actor,
		CreatedAt:   now,
		CompletedAt: func() *time.Time { t := now; return &t }(),
	}
	for _, tx := range preview.Transactions {
		if !tx.WouldDelete {
			continue
		}
		markedAt := now
		recoverableUntil := deadline
		action := "marked"
		var sweptAt *time.Time
		if preview.AsOf.Sub(now) >= time.Duration(recoveryDays)*24*time.Hour {
			s := now
			sweptAt = &s
			action = "swept"
			run.SweptTransactionCount++
		} else {
			run.MarkedTransactionCount++
		}
		requiresDelete := false
		if tx.PolicyID != nil && preview.EffectivePolicy != nil && *tx.PolicyID == preview.EffectivePolicy.ID && preview.EffectivePolicy.AllowLatestViewDeletion {
			requiresDelete = true
			run.DeleteTransactionCount++
		}
		reason := "retention policy matched transaction"
		if tx.Reason != nil {
			reason = *tx.Reason
		}
		run.Items = append(run.Items, models.RetentionExecutionItem{
			ID:                        uuid.New(),
			RunID:                     run.ID,
			PolicyID:                  tx.PolicyID,
			TransactionID:             tx.ID,
			Action:                    action,
			Reason:                    reason,
			MarkedAt:                  &markedAt,
			RecoverableUntil:          &recoverableUntil,
			SweptAt:                   sweptAt,
			RequiresDeleteTransaction: requiresDelete,
		})
	}
	if run.DeleteTransactionCount > 0 {
		run.Warnings = append(run.Warnings, "One or more policies may remove current-view data; local dataset semantics should record DELETE transactions for those removals.")
	}
	return run
}

func RunExecution(ctx context.Context, db *pgxpool.Pool, request *models.RunRetentionExecutionRequest, scope OrgScope, claims *authmw.Claims) (*models.RetentionExecutionRun, error) {
	policies, err := LoadPolicies(ctx, db, scope)
	if err != nil {
		return nil, err
	}
	resolved := ResolveApplicable(policies, request.DatasetRid, &models.ResolutionContext{ProjectID: request.ProjectID, MarkingID: request.MarkingID, SpaceID: request.SpaceID, OrgID: request.OrgID})
	preview, err := RunPreview(ctx, db, request.DatasetRid, request.AsOfDays, &resolved)
	if err != nil {
		return nil, err
	}
	actor := ""
	if claims != nil {
		actor = claims.Sub.String()
	}
	run := BuildExecutionRun(preview, request, actor, scope.OrgID)
	if !request.DryRun {
		_ = applyExecutionMarks(ctx, db, &run)
	}
	_ = persistExecutionRun(ctx, db, &run)
	return &run, nil
}

func applyExecutionMarks(ctx context.Context, db *pgxpool.Pool, run *models.RetentionExecutionRun) error {
	for _, item := range run.Items {
		meta, _ := json.Marshal(map[string]any{"run_id": run.ID, "action": item.Action, "recoverable_until": item.RecoverableUntil})
		_, _ = db.Exec(ctx, `UPDATE dataset_transactions SET metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object('retention_execution', $2::jsonb) WHERE id = $1`, item.TransactionID, meta)
		if item.SweptAt != nil {
			_, _ = db.Exec(ctx, `UPDATE dataset_files SET deleted_at = COALESCE(deleted_at, $2) WHERE transaction_id = $1`, item.TransactionID, *item.SweptAt)
		}
	}
	return nil
}

func persistExecutionRun(ctx context.Context, db *pgxpool.Pool, run *models.RetentionExecutionRun) error {
	warnings, _ := json.Marshal(run.Warnings)
	_, err := db.Exec(ctx, `INSERT INTO retention_execution_runs (id, org_id, dataset_rid, status, dry_run, marked_transaction_count, swept_transaction_count, delete_transaction_count, recovery_window_days, remediation_deadline, irreversible_after, warnings, created_by, created_at, completed_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, run.ID, run.OrgID, run.DatasetRid, run.Status, run.DryRun, run.MarkedTransactionCount, run.SweptTransactionCount, run.DeleteTransactionCount, run.RecoveryWindowDays, run.RemediationDeadline, run.IrreversibleAfter, warnings, run.CreatedBy, run.CreatedAt, run.CompletedAt)
	if err != nil {
		return err
	}
	for _, item := range run.Items {
		_, _ = db.Exec(ctx, `INSERT INTO retention_execution_items (id, run_id, policy_id, transaction_id, action, reason, marked_at, recoverable_until, swept_at, requires_delete_transaction) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, item.ID, item.RunID, item.PolicyID, item.TransactionID, item.Action, item.Reason, item.MarkedAt, item.RecoverableUntil, item.SweptAt, item.RequiresDeleteTransaction)
	}
	return nil
}

func executionSummary(run *models.RetentionExecutionRun) string {
	return fmt.Sprintf("marked=%d swept=%d delete_transactions=%d", run.MarkedTransactionCount, run.SweptTransactionCount, run.DeleteTransactionCount)
}
