package retentionpolicy

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

func TestBuildExecutionRunExposesRecoveryAndIrreversibleSweep(t *testing.T) {
	t.Parallel()
	policyID := uuid.New()
	txID := uuid.New()
	reason := "retention_days=30"
	preview := models.RetentionPreviewResponse{
		DatasetRid: "ri.datasets.main.sample",
		AsOf:       time.Now().UTC().AddDate(0, 0, 8),
		EffectivePolicy: &models.RetentionPolicy{
			ID:                      policyID,
			Name:                    "current data deletion",
			AllowLatestViewDeletion: true,
		},
		Transactions: []models.RetentionPreviewTransaction{{
			ID:          txID,
			WouldDelete: true,
			PolicyID:    &policyID,
			Reason:      &reason,
		}},
	}

	run := BuildExecutionRun(preview, &models.RunRetentionExecutionRequest{DatasetRid: preview.DatasetRid, RecoveryWindowDays: 7}, "operator", nil)

	require.Equal(t, "completed", run.Status)
	require.Equal(t, 0, run.MarkedTransactionCount)
	require.Equal(t, 1, run.SweptTransactionCount)
	require.Equal(t, 1, run.DeleteTransactionCount)
	require.NotNil(t, run.RemediationDeadline)
	require.NotNil(t, run.IrreversibleAfter)
	require.Len(t, run.Items, 1)
	require.Equal(t, "swept", run.Items[0].Action)
	require.True(t, run.Items[0].RequiresDeleteTransaction)
	require.Contains(t, run.Warnings[len(run.Warnings)-1], "DELETE transactions")
}
