package handler_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/domain/executionmode"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/handler"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/models"
)

func createModule(t *testing.T, r http.Handler, mode models.ExecutionMode) models.ComputeModule {
	t.Helper()
	body := handler.CreateComputeModuleRequest{
		Name:          "mod-" + uuid.New().String()[:6],
		ProjectID:     uuid.New(),
		ExecutionMode: mode,
	}
	w := doJSON(t, r, http.MethodPost, "/api/v1/compute-modules", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed module: %d %s", w.Code, w.Body.String())
	}
	return decode[models.ComputeModule](t, w)
}

func TestGetExecutionModeAffordances(t *testing.T) {
	r, _ := buildTestRouter(t)

	fn := createModule(t, r, models.ExecutionModeFunction)
	w := doJSON(t, r, http.MethodGet, "/api/v1/compute-modules/"+fn.ID.String()+"/execution-mode", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", w.Code, w.Body.String())
	}
	snap := decode[executionmode.Snapshot](t, w)
	if snap.Mode != models.ExecutionModeFunction || !snap.Affordances.SupportsFunctionInvocation {
		t.Fatalf("function snapshot looks wrong: %+v", snap)
	}
	if snap.Affordances.SupportsPipelineInputConfig {
		t.Fatalf("function module should not advertise pipeline affordance")
	}

	pipe := createModule(t, r, models.ExecutionModePipeline)
	w = doJSON(t, r, http.MethodGet, "/api/v1/compute-modules/"+pipe.ID.String()+"/execution-mode", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", w.Code, w.Body.String())
	}
	snap = decode[executionmode.Snapshot](t, w)
	if snap.Mode != models.ExecutionModePipeline || !snap.Affordances.SupportsPipelineInputConfig {
		t.Fatalf("pipeline snapshot looks wrong: %+v", snap)
	}
}

func TestPipelineIOConfigBlockedOnFunctionMode(t *testing.T) {
	r, _ := buildTestRouter(t)
	fn := createModule(t, r, models.ExecutionModeFunction)

	cfg := models.PipelineIOConfig{
		Inputs: []models.PipelineIO{{
			Alias:        "events",
			ResourceKind: models.PipelineResourceStream,
			ResourceID:   uuid.New(),
		}},
	}
	w := doJSON(t, r, http.MethodPut, "/api/v1/compute-modules/"+fn.ID.String()+"/pipeline-io", cfg)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	type errBody struct {
		Error        string               `json:"error"`
		RequiredMode models.ExecutionMode `json:"required_mode"`
	}
	got := decode[errBody](t, w)
	if got.RequiredMode != models.ExecutionModePipeline {
		t.Fatalf("error body should advertise required mode: %+v", got)
	}

	// Same guard on the clear path.
	w = doJSON(t, r, http.MethodDelete, "/api/v1/compute-modules/"+fn.ID.String()+"/pipeline-io", nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 on clear, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestPipelineIOConfigRoundTripOnPipelineMode(t *testing.T) {
	r, _ := buildTestRouter(t)
	pipe := createModule(t, r, models.ExecutionModePipeline)

	streamID := uuid.New()
	cfg := models.PipelineIOConfig{
		Inputs: []models.PipelineIO{{
			Alias:        "events",
			ResourceKind: models.PipelineResourceStream,
			ResourceID:   streamID,
		}},
		Outputs: []models.PipelineIO{{
			Alias:        "audit",
			ResourceKind: models.PipelineResourceDataset,
			ResourceID:   uuid.New(),
		}},
	}
	w := doJSON(t, r, http.MethodPut, "/api/v1/compute-modules/"+pipe.ID.String()+"/pipeline-io", cfg)
	if w.Code != http.StatusOK {
		t.Fatalf("set: %d %s", w.Code, w.Body.String())
	}
	updated := decode[models.ComputeModule](t, w)
	if updated.PipelineIOConfig == nil ||
		len(updated.PipelineIOConfig.Inputs) != 1 ||
		updated.PipelineIOConfig.Inputs[0].ResourceID != streamID {
		t.Fatalf("pipeline I/O not persisted: %+v", updated.PipelineIOConfig)
	}

	w = doJSON(t, r, http.MethodDelete, "/api/v1/compute-modules/"+pipe.ID.String()+"/pipeline-io", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", w.Code, w.Body.String())
	}
	cleared := decode[models.ComputeModule](t, w)
	if cleared.PipelineIOConfig != nil {
		t.Fatalf("expected nil PipelineIOConfig after clear, got %+v", cleared.PipelineIOConfig)
	}
}

func TestPipelineIOConfigInvalidPayload(t *testing.T) {
	r, _ := buildTestRouter(t)
	pipe := createModule(t, r, models.ExecutionModePipeline)

	// No inputs and no outputs is not a useful configuration.
	w := doJSON(t, r, http.MethodPut, "/api/v1/compute-modules/"+pipe.ID.String()+"/pipeline-io", models.PipelineIOConfig{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on empty cfg, got %d body=%s", w.Code, w.Body.String())
	}

	// Duplicate alias should also fail validation.
	dup := models.PipelineIOConfig{
		Inputs: []models.PipelineIO{
			{Alias: "in", ResourceKind: models.PipelineResourceStream, ResourceID: uuid.New()},
			{Alias: "IN", ResourceKind: models.PipelineResourceDataset, ResourceID: uuid.New()},
		},
	}
	w = doJSON(t, r, http.MethodPut, "/api/v1/compute-modules/"+pipe.ID.String()+"/pipeline-io", dup)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on duplicate alias, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestInvokeFunctionBlockedOnPipelineMode(t *testing.T) {
	r, _, _ := buildTestRouterWithDispatcher(t)
	pipe := createModule(t, r, models.ExecutionModePipeline)

	w := doJSON(t, r, http.MethodPost,
		"/api/v1/compute-modules/"+pipe.ID.String()+"/functions/echo/invoke",
		map[string]any{"payload": map[string]any{"any": "payload"}})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	type errBody struct {
		Error        string               `json:"error"`
		RequiredMode models.ExecutionMode `json:"required_mode"`
	}
	got := decode[errBody](t, w)
	if got.RequiredMode != models.ExecutionModeFunction {
		t.Fatalf("error body should advertise required mode: %+v", got)
	}
}
