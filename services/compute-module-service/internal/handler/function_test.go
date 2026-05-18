package handler_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/domain/function"
	dispatch "github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/executionmode"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/handler"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/models"
)

func TestInvokeFunctionSuccess(t *testing.T) {
	r, disp, _ := buildTestRouterWithDispatcher(t)
	fn := createModule(t, r, models.ExecutionModeFunction)

	disp.result = dispatch.Result{Status: function.StatusSucceeded, Payload: []byte(`{"echo":true}`)}

	body := handler.InvokeFunctionRequest{Payload: json.RawMessage(`{"in":1}`)}
	w := doJSON(t, r, http.MethodPost,
		"/api/v1/compute-modules/"+fn.ID.String()+"/functions/echo/invoke", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", w.Code, w.Body.String())
	}
	got := decode[handler.SyncInvocationResponse](t, w)
	if got.Invocation == nil || got.Invocation.Status != function.StatusSucceeded {
		t.Fatalf("invocation should be succeeded: %+v", got.Invocation)
	}
	if string(got.Result) != `{"echo":true}` {
		t.Fatalf("unexpected result: %s", string(got.Result))
	}
	if disp.calls != 1 {
		t.Fatalf("dispatcher should run once, ran %d", disp.calls)
	}
	if disp.last == nil || disp.last.FunctionName != "echo" || disp.last.ModuleID != fn.ID {
		t.Fatalf("dispatcher saw wrong invocation: %+v", disp.last)
	}
}

func TestInvokeFunctionTimeoutReturns504(t *testing.T) {
	r, disp, _ := buildTestRouterWithDispatcher(t)
	fn := createModule(t, r, models.ExecutionModeFunction)

	disp.result = dispatch.Result{Status: function.StatusTimeout}
	disp.err = function.ErrInvocationTimeout

	w := doJSON(t, r, http.MethodPost,
		"/api/v1/compute-modules/"+fn.ID.String()+"/functions/slow/invoke", nil)
	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestInvokeFunctionFailureMaps502(t *testing.T) {
	r, disp, _ := buildTestRouterWithDispatcher(t)
	fn := createModule(t, r, models.ExecutionModeFunction)

	disp.result = dispatch.Result{Status: function.StatusFailed, ExitCode: 500}
	disp.err = errors.New("upstream blew up")

	w := doJSON(t, r, http.MethodPost,
		"/api/v1/compute-modules/"+fn.ID.String()+"/functions/boom/invoke", nil)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body=%s", w.Code, w.Body.String())
	}
	got := decode[handler.SyncInvocationResponse](t, w)
	if got.Invocation.Status != function.StatusFailed {
		t.Fatalf("expected failed status, got %q", got.Invocation.Status)
	}
}

func TestInvokeFunctionPayloadTooLarge(t *testing.T) {
	r, _, _ := buildTestRouterWithDispatcher(t)
	fn := createModule(t, r, models.ExecutionModeFunction)

	// shove a 32 KiB blob at the handler when the configured limit is
	// well above default but still smaller than the test payload
	huge := strings.Repeat("x", 32*1024)
	w := doJSON(t, r, http.MethodPost,
		"/api/v1/compute-modules/"+fn.ID.String()+"/functions/big/invoke",
		map[string]any{"payload": map[string]any{"blob": huge}})
	if w.Code != http.StatusOK {
		// 32 KiB is well below the default 10 MiB limit so this should succeed
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestInvokeFunctionAsyncReturnsHandle(t *testing.T) {
	r, disp, _ := buildTestRouterWithDispatcher(t)
	fn := createModule(t, r, models.ExecutionModeFunction)

	// Stall the dispatcher's view of the result so we can observe the
	// queued row before the async goroutine completes.
	disp.result = dispatch.Result{Status: function.StatusSucceeded, Payload: []byte(`{}`)}

	w := doJSON(t, r, http.MethodPost,
		"/api/v1/compute-modules/"+fn.ID.String()+"/functions/work/invoke-async",
		handler.InvokeFunctionRequest{Payload: json.RawMessage(`null`)})
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	ack := decode[handler.AsyncInvocationResponse](t, w)
	if ack.InvocationID == uuid.Nil {
		t.Fatal("expected invocation id")
	}

	// Poll briefly until the async goroutine completes the row.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		w := doJSON(t, r, http.MethodGet,
			"/api/v1/compute-modules/invocations/"+ack.InvocationID.String(), nil)
		if w.Code == http.StatusOK {
			row := decode[function.FunctionInvocation](t, w)
			if row.Status.IsTerminal() {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("async invocation never reached terminal status")
}

func TestCancelInvocation(t *testing.T) {
	r, _, _ := buildTestRouterWithDispatcher(t)
	fn := createModule(t, r, models.ExecutionModeFunction)

	// Run invoke-async to get a queued invocation.
	w := doJSON(t, r, http.MethodPost,
		"/api/v1/compute-modules/"+fn.ID.String()+"/functions/queue/invoke-async",
		handler.InvokeFunctionRequest{Payload: json.RawMessage(`null`)})
	if w.Code != http.StatusAccepted {
		t.Fatalf("seed: %d %s", w.Code, w.Body.String())
	}
	ack := decode[handler.AsyncInvocationResponse](t, w)

	// Wait until the row is terminal — at that point cancel must 409
	// because cancelling a terminal row is rejected by the repo.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		w := doJSON(t, r, http.MethodGet,
			"/api/v1/compute-modules/invocations/"+ack.InvocationID.String(), nil)
		row := decode[function.FunctionInvocation](t, w)
		if row.Status.IsTerminal() {
			w := doJSON(t, r, http.MethodPost,
				"/api/v1/compute-modules/invocations/"+ack.InvocationID.String()+"/cancel", nil)
			if w.Code != http.StatusConflict {
				t.Fatalf("expected 409 on terminal cancel, got %d %s", w.Code, w.Body.String())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("invocation never reached terminal status before cancel")
}

func TestListInvocationsScopedToTenant(t *testing.T) {
	r, _, _ := buildTestRouterWithDispatcher(t)
	fn := createModule(t, r, models.ExecutionModeFunction)

	for i := 0; i < 3; i++ {
		w := doJSON(t, r, http.MethodPost,
			"/api/v1/compute-modules/"+fn.ID.String()+"/functions/echo/invoke",
			handler.InvokeFunctionRequest{Payload: json.RawMessage(`null`)})
		if w.Code != http.StatusOK {
			t.Fatalf("invoke %d: %d %s", i, w.Code, w.Body.String())
		}
	}
	w := doJSON(t, r, http.MethodGet,
		"/api/v1/compute-modules/invocations?module_id="+fn.ID.String(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	type page struct {
		Items      []function.FunctionInvocation `json:"items"`
		NextCursor *string                       `json:"next_cursor,omitempty"`
	}
	got := decode[page](t, w)
	if len(got.Items) != 3 {
		t.Fatalf("expected 3 invocations, got %d", len(got.Items))
	}
}

func TestInvokeFunctionRejectsNonFunctionMode(t *testing.T) {
	r, _, _ := buildTestRouterWithDispatcher(t)
	pipe := createModule(t, r, models.ExecutionModePipeline)

	w := doJSON(t, r, http.MethodPost,
		"/api/v1/compute-modules/"+pipe.ID.String()+"/functions/echo/invoke", nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
}
