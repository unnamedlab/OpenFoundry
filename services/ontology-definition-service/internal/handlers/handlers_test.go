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
	"github.com/openfoundry/openfoundry-go/services/ontology-definition-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/ontology-definition-service/internal/models"
)

func TestObjectTypeJSONShape(t *testing.T) {
	t.Parallel()
	icon := "person"
	color := "#abcdef"
	pk := "id"
	v := models.ObjectType{
		ID: uuid.New(), Name: "Customer", DisplayName: "Customer",
		Description: "Buyer of products.", PrimaryKeyProperty: &pk,
		Icon: &icon, Color: &color, OwnerID: uuid.New(),
		CreatedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(v)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "name", "display_name", "description", "primary_key_property",
		"icon", "color", "owner_id", "created_at", "updated_at",
	} {
		assert.Contains(t, view, k)
	}
}

func TestCreateObjectTypeRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("POST", "/object-types",
		strings.NewReader(`{"name":"x","display_name":"y"}`))
	rec := httptest.NewRecorder()
	h.CreateObjectType(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestCreateObjectTypeRejectsEmptyFields(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	c := &authmw.Claims{Sub: uuid.New()}
	req := httptest.NewRequest("POST", "/object-types",
		strings.NewReader(`{"name":"","display_name":""}`))
	req = req.WithContext(authmw.ContextWithClaims(context.Background(), c))
	rec := httptest.NewRecorder()
	h.CreateObjectType(rec, req)
	assert.Equal(t, 400, rec.Code)
}

func TestListObjectTypesRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest("GET", "/object-types", nil)
	rec := httptest.NewRecorder()
	h.ListObjectTypes(rec, req)
	assert.Equal(t, 401, rec.Code)
}
