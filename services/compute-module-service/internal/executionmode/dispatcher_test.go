package executionmode_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/domain/function"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/executionmode"
)

func newInvocation(moduleID uuid.UUID, fnName, body string) *function.FunctionInvocation {
	return &function.FunctionInvocation{
		ID:           uuid.New(),
		ModuleID:     moduleID,
		FunctionName: fnName,
		Payload:      json.RawMessage(body),
		TenantID:     uuid.New(),
		ActorID:      uuid.New(),
		ScheduledAt:  time.Now().UTC(),
		Status:       function.StatusQueued,
	}
}

func TestHTTPDispatcherSuccess(t *testing.T) {
	moduleID := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if r.Header.Get("X-Invocation-Id") == "" {
			t.Fatal("missing invocation header")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"echo":"ok"}`))
	}))
	defer srv.Close()

	res := executionmode.NewStaticEndpointResolver()
	res.Register(moduleID, srv.URL)
	d := executionmode.NewHTTPDispatcher(res, executionmode.HTTPDispatcherConfig{Timeout: time.Second})

	inv := newInvocation(moduleID, "echo", `{"hello":"world"}`)
	result, err := d.Dispatch(context.Background(), inv)
	if err != nil {
		t.Fatalf("dispatch returned err: %v", err)
	}
	if result.Status != function.StatusSucceeded {
		t.Fatalf("expected succeeded, got %q", result.Status)
	}
	if string(result.Payload) != `{"echo":"ok"}` {
		t.Fatalf("unexpected payload: %s", string(result.Payload))
	}
}

func TestHTTPDispatcherTimeout(t *testing.T) {
	moduleID := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the dispatcher's context (propagated server-side
		// by net/http) cancels. This both validates that the dispatcher
		// abandons the call on timeout and lets srv.Close() return.
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := executionmode.NewStaticEndpointResolver()
	res.Register(moduleID, srv.URL)
	d := executionmode.NewHTTPDispatcher(res, executionmode.HTTPDispatcherConfig{Timeout: 50 * time.Millisecond})

	inv := newInvocation(moduleID, "slow", `null`)
	result, err := d.Dispatch(context.Background(), inv)
	if !errors.Is(err, function.ErrInvocationTimeout) {
		t.Fatalf("expected ErrInvocationTimeout, got %v", err)
	}
	if result.Status != function.StatusTimeout {
		t.Fatalf("expected timeout status, got %q", result.Status)
	}
}

func TestHTTPDispatcherPayloadTooLarge(t *testing.T) {
	moduleID := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	res := executionmode.NewStaticEndpointResolver()
	res.Register(moduleID, srv.URL)
	d := executionmode.NewHTTPDispatcher(res, executionmode.HTTPDispatcherConfig{
		Timeout:        time.Second,
		BodyLimitBytes: 8,
	})

	inv := newInvocation(moduleID, "noop", `{"k":"`+strings.Repeat("x", 64)+`"}`)
	_, err := d.Dispatch(context.Background(), inv)
	if !errors.Is(err, function.ErrPayloadTooLarge) {
		t.Fatalf("expected ErrPayloadTooLarge, got %v", err)
	}
}

func TestHTTPDispatcherUnknownEndpoint(t *testing.T) {
	d := executionmode.NewHTTPDispatcher(executionmode.NewStaticEndpointResolver(), executionmode.HTTPDispatcherConfig{Timeout: time.Second})
	_, err := d.Dispatch(context.Background(), newInvocation(uuid.New(), "echo", `null`))
	if !errors.Is(err, function.ErrModuleVersionInactive) {
		t.Fatalf("expected ErrModuleVersionInactive, got %v", err)
	}
}

func TestHTTPDispatcherUpstreamStatusMapping(t *testing.T) {
	moduleID := uuid.New()

	cases := []struct {
		name      string
		status    int
		wantErr   error
		wantState function.Status
	}{
		{"timeout 504", http.StatusGatewayTimeout, function.ErrInvocationTimeout, function.StatusTimeout},
		{"not found 404", http.StatusNotFound, function.ErrFunctionNotFound, function.StatusFailed},
		{"conflict 409", http.StatusConflict, function.ErrModuleVersionInactive, function.StatusFailed},
		{"too large 413", http.StatusRequestEntityTooLarge, function.ErrPayloadTooLarge, function.StatusFailed},
		{"server 500", http.StatusInternalServerError, nil, function.StatusFailed},
		{"ok 200", http.StatusOK, nil, function.StatusSucceeded},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			res := executionmode.NewStaticEndpointResolver()
			res.Register(moduleID, srv.URL)
			d := executionmode.NewHTTPDispatcher(res, executionmode.HTTPDispatcherConfig{Timeout: time.Second})

			result, err := d.Dispatch(context.Background(), newInvocation(moduleID, "echo", `null`))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("want %v, got %v", tc.wantErr, err)
				}
			} else if err != nil {
				t.Fatalf("did not expect err, got %v", err)
			}
			if result.Status != tc.wantState {
				t.Fatalf("status: want %q got %q", tc.wantState, result.Status)
			}
		})
	}
}

func TestCancelAtBestEffort(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("expected DELETE, got %s", r.Method)
		}
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := executionmode.CancelAt(context.Background(), nil, srv.URL, uuid.New()); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("expected one cancel hit, got %d", hits)
	}
}

func TestHTTPDispatcherContextCancel(t *testing.T) {
	moduleID := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Match the timeout test: bail out when the server context fires
		// or when an upper bound elapses, so srv.Close() never wedges.
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := executionmode.NewStaticEndpointResolver()
	res.Register(moduleID, srv.URL)
	d := executionmode.NewHTTPDispatcher(res, executionmode.HTTPDispatcherConfig{Timeout: 2 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	result, err := d.Dispatch(ctx, newInvocation(moduleID, "wait", `null`))
	if err == nil {
		t.Fatal("expected an error after cancel")
	}
	if result.Status != function.StatusCancelled {
		t.Fatalf("expected cancelled status, got %q", result.Status)
	}
}
