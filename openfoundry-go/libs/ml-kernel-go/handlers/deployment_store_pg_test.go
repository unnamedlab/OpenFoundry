package handlers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	pgxmock "github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

func TestPGDeploymentStoreCreateGetUpdateWithMockedPostgres(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	store := &PGDeploymentStore{Pool: mock}
	ctx := context.Background()
	now := time.Now().UTC()
	modelID := uuid.New()
	versionID := uuid.New()
	deploymentID := uuid.New()
	split := []models.TrafficSplitEntry{{ModelVersionID: versionID, Label: "champion", Allocation: 100}}
	splitJSON, err := json.Marshal(split)
	require.NoError(t, err)

	createdRows := pgxmock.NewRows(deploymentColumnNames()).AddRow(
		deploymentID, modelID, "fraud-prod", "active", "single", "/predict/fraud",
		splitJSON, "24h", nil, []byte("null"), now, now,
	)
	mock.ExpectQuery("INSERT INTO ml_deployments").WithArgs(deploymentID, modelID, "fraud-prod", "active", "single", "/predict/fraud", pgxmock.AnyArg(), "24h", pgxmock.AnyArg()).WillReturnRows(createdRows)

	created, err := store.CreateDeployment(ctx, models.ModelDeployment{
		ID:               deploymentID,
		ModelID:          modelID,
		Name:             "fraud-prod",
		Status:           "active",
		StrategyType:     "single",
		EndpointPath:     "/predict/fraud",
		TrafficSplit:     split,
		MonitoringWindow: "24h",
	})
	require.NoError(t, err)
	assert.Equal(t, deploymentID, created.ID)
	assert.Equal(t, split, created.TrafficSplit)

	getRows := pgxmock.NewRows(deploymentColumnNames()).AddRow(
		deploymentID, modelID, "fraud-prod", "active", "single", "/predict/fraud",
		splitJSON, "24h", nil, []byte("null"), now, now,
	)
	mock.ExpectQuery("SELECT (.+) FROM ml_deployments WHERE id").WithArgs(deploymentID).WillReturnRows(getRows)

	got, err := store.GetDeployment(ctx, deploymentID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "fraud-prod", got.Name)

	updatedRows := pgxmock.NewRows(deploymentColumnNames()).AddRow(
		deploymentID, modelID, "fraud-paused", "paused", "single", "/predict/fraud",
		splitJSON, "24h", nil, []byte("null"), now, now.Add(time.Minute),
	)
	mock.ExpectQuery("UPDATE ml_deployments SET").WithArgs(deploymentID, "fraud-paused", "paused", "single", "/predict/fraud", pgxmock.AnyArg(), "24h", pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnRows(updatedRows)

	created.Name = "fraud-paused"
	created.Status = "paused"
	updated, err := store.UpdateDeployment(ctx, created)
	require.NoError(t, err)
	assert.Equal(t, "paused", updated.Status)
	assert.Equal(t, "fraud-paused", updated.Name)

	require.NoError(t, mock.ExpectationsWereMet())
}

func deploymentColumnNames() []string {
	return []string{
		"id", "model_id", "name", "status", "strategy_type",
		"endpoint_path", "traffic_split", "monitoring_window",
		"baseline_dataset_id", "drift_report", "created_at", "updated_at",
	}
}
