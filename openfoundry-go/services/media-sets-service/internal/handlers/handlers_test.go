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
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
)

func TestMediaSetJSONShape(t *testing.T) {
	t.Parallel()
	src := "ri.foundry.main.virtual.x"
	v := models.MediaSet{
		RID: "ri.foundry.main.media_set." + uuid.NewString(),
		ProjectRID: "ri.foundry.main.project.p", Name: "scans", Schema: "IMAGE",
		AllowedMimeTypes:  []string{"image/png", "image/jpeg"},
		TransactionPolicy: "TRANSACTIONAL", RetentionSeconds: 0, Virtual: true,
		SourceRID: &src,
		Markings:  []string{"public"},
		CreatedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		CreatedBy: "u",
	}
	out, err := json.Marshal(v)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"rid", "project_rid", "name", "schema", "allowed_mime_types",
		"transaction_policy", "retention_seconds", "virtual", "source_rid",
		"markings", "created_at", "created_by",
	} {
		assert.Contains(t, view, k)
	}
}

func TestCreateMediaSetRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("POST", "/media-sets",
		strings.NewReader(`{"project_rid":"x","name":"y","schema":"IMAGE"}`))
	rec := httptest.NewRecorder()
	h.CreateMediaSet(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestCreateMediaSetRejectsBadSchema(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/media-sets",
		strings.NewReader(`{"project_rid":"p","name":"n","schema":"BANANA"}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateMediaSet(rec, req)
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "schema")
}

func TestCreateMediaSetRejectsBadTxPolicy(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/media-sets",
		strings.NewReader(`{"project_rid":"p","name":"n","schema":"IMAGE","transaction_policy":"NONSENSE"}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateMediaSet(rec, req)
	assert.Equal(t, 400, rec.Code)
}

func TestListMediaSetsRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("GET", "/media-sets", nil)
	rec := httptest.NewRecorder()
	h.ListMediaSets(rec, req)
	assert.Equal(t, 401, rec.Code)
}
