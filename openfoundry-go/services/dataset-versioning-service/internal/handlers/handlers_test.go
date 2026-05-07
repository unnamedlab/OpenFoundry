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
	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/models"
)

func TestDatasetJSONShape(t *testing.T) {
	t.Parallel()
	d := models.Dataset{
		ID: uuid.New(), Name: "sales", Description: "fact",
		Format: "parquet", StoragePath: "s3://x/y",
		SizeBytes: 1024, RowCount: 100,
		OwnerID:        uuid.New(),
		Tags:           []string{"finance", "daily"},
		CurrentVersion: 1,
		CreatedAt:      time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(d)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "name", "description", "format", "storage_path", "size_bytes",
		"row_count", "owner_id", "tags", "current_version", "created_at", "updated_at",
	} {
		assert.Contains(t, view, k)
	}
	assert.Equal(t, "parquet", view["format"])
}

func TestCreateDatasetRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("POST", "/datasets",
		strings.NewReader(`{"name":"x","storage_path":"s3://y"}`))
	rec := httptest.NewRecorder()
	h.CreateDataset(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestCreateDatasetRejectsEmptyFields(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/datasets",
		strings.NewReader(`{"name":"","storage_path":""}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateDataset(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "name and storage_path required")
}

func TestListDatasetsRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("GET", "/datasets", nil)
	rec := httptest.NewRecorder()
	h.ListDatasets(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestListDatasetsRejectsBadOwnerID(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("GET", "/datasets?owner_id=not-uuid", nil)
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.ListDatasets(rec, req)
	assert.Equal(t, 400, rec.Code)
}
