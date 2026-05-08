package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

func TestCreateActionWhatIfBranchPlansPreviewAndSnapshots(t *testing.T) {
	ctx := context.Background()
	state := newTestState(t)
	claims := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}

	objectTypeID := uuid.New()
	objectID := uuid.New()
	seedObjectTypeDefinition(t, state, objectTypeID)
	seedPropertyDefinition(t, state, objectTypeID, "status", "string")

	nowMs := time.Now().UTC().UnixMilli()
	owner := storage.OwnerId(claims.Sub.String())
	_, err := state.Stores.Objects.Put(ctx, storage.Object{
		Tenant:      storage.TenantId("default"),
		ID:          storage.ObjectId(objectID.String()),
		TypeID:      storage.TypeId(objectTypeID.String()),
		Version:     1,
		Payload:     json.RawMessage(`{"status":"open"}`),
		UpdatedAtMs: nowMs,
		Owner:       &owner,
		Markings:    []storage.MarkingId{"public"},
	}, nil)
	if err != nil {
		t.Fatalf("seed object: %v", err)
	}

	action := models.ActionType{
		ID:            uuid.New(),
		Name:          "close_case",
		DisplayName:   "Close case",
		ObjectTypeID:  objectTypeID,
		OperationKind: "update_object",
		Config:        json.RawMessage(`{"property_mappings":[],"static_patch":{"status":"closed"}}`),
		OwnerID:       claims.Sub,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if _, err := domain.PutAction(ctx, state.Stores.Definitions, action); err != nil {
		t.Fatalf("seed action: %v", err)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", action.ID.String())
	reqBody := []byte(`{"target_object_id":"` + objectID.String() + `","parameters":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/ontology/actions/"+action.ID.String()+"/what-if", bytes.NewReader(reqBody))
	req = req.WithContext(context.WithValue(authmw.ContextWithClaims(req.Context(), claims), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	CreateActionWhatIfBranch(state).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var branch models.ActionWhatIfBranch
	if err := json.Unmarshal(rec.Body.Bytes(), &branch); err != nil {
		t.Fatalf("decode branch: %v", err)
	}
	if branch.Deleted {
		t.Fatal("update what-if branch should not be marked deleted")
	}
	if len(branch.BeforeObject) == 0 || len(branch.AfterObject) == 0 {
		t.Fatalf("expected before/after snapshots, got before=%s after=%s", branch.BeforeObject, branch.AfterObject)
	}

	var preview map[string]any
	if err := json.Unmarshal(branch.Preview, &preview); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	if preview["kind"] != "update_object" {
		t.Fatalf("preview kind=%v", preview["kind"])
	}
	patch := preview["patch"].(map[string]any)
	if patch["status"] != "closed" {
		t.Fatalf("preview patch=%v", patch)
	}

	var before, after struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(branch.BeforeObject, &before); err != nil {
		t.Fatalf("decode before: %v", err)
	}
	if err := json.Unmarshal(branch.AfterObject, &after); err != nil {
		t.Fatalf("decode after: %v", err)
	}
	if before.Properties["status"] != "open" || after.Properties["status"] != "closed" {
		t.Fatalf("snapshot status drift: before=%v after=%v", before.Properties, after.Properties)
	}
}
