package handlers_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/models"
)

func TestIngestJobJSONShape(t *testing.T) {
	t.Parallel()
	kafka := "kafka-connector-1"
	flink := "flink-deployment-1"
	j := models.IngestJob{
		ID: uuid.New(), Name: "sales-cdc", Namespace: "data",
		Spec: json.RawMessage(`{"source":"snowflake"}`), Status: "running",
		KafkaConnectorName:  &kafka,
		FlinkDeploymentName: &flink,
		CreatedAt:           time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		UpdatedAt:           time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(j)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "name", "namespace", "spec", "status",
		"kafka_connector_name", "flink_deployment_name", "error",
		"created_at", "updated_at",
	} {
		assert.Contains(t, view, k)
	}
}

func TestCreateIngestJobRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("POST", "/ingest-jobs",
		strings.NewReader(`{"name":"x","namespace":"y","spec":{}}`))
	rec := httptest.NewRecorder()
	h.CreateIngestJob(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestCreateIngestJobRejectsEmptyFields(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/ingest-jobs",
		strings.NewReader(`{"name":"","namespace":""}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateIngestJob(rec, req)
	assert.Equal(t, 400, rec.Code)
}

func TestCreateIngestJobRejectsMissingSpec(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/ingest-jobs",
		strings.NewReader(`{"name":"a","namespace":"b"}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateIngestJob(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "spec")
}

func TestListIngestJobsRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("GET", "/ingest-jobs", nil)
	rec := httptest.NewRecorder()
	h.ListIngestJobs(rec, req)
	assert.Equal(t, 401, rec.Code)
}
