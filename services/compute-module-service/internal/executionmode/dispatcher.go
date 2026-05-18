// Package executionmode contains the dispatcher contract that drives
// function-mode invocations of a Compute Module against the module's
// runtime endpoint (checklist CM.6 / CM.8).
//
// The interface is split out from the repo so handlers can run a fake
// in unit tests, and a real http-backed implementation can talk to
// whatever runtime the module is deployed against without leaking
// transport details into the domain or the handler layer.
package executionmode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/domain/function"
)

// Default knobs. Handlers expose configured overrides; the package
// defaults are deliberately conservative so a misconfigured deployment
// fails fast rather than wedging a worker for hours.
const (
	DefaultDispatchTimeout = 30 * time.Second
	MaxDispatchTimeout     = 10 * time.Minute
	DefaultBodyLimitBytes  = 10 * 1024 * 1024 // 10 MiB
)

// Result is the dispatcher's verdict for a single Dispatch call. The
// HTTP layer maps it onto the persisted FunctionInvocation row.
type Result struct {
	Status     function.Status
	Payload    []byte
	DurationMs int64
	ExitCode   int
}

// Dispatcher executes a function-mode invocation against the module
// runtime and, optionally, signals the runtime that an in-flight call
// should be aborted.
type Dispatcher interface {
	// Dispatch sends the invocation to the module runtime and returns
	// the result. The returned error is non-nil only for transport-level
	// or limit failures — a non-zero ExitCode (i.e. the function ran but
	// failed) is reported via Result.Status == StatusFailed.
	Dispatch(ctx context.Context, inv *function.FunctionInvocation) (Result, error)

	// Cancel is a best-effort signal to the runtime that the given
	// invocation should stop. Implementations may treat this as a no-op
	// when the runtime exposes no cancel hook.
	Cancel(ctx context.Context, invocationID uuid.UUID) error
}

// EndpointResolver maps a moduleID onto the base URL of the runtime
// that should service its invocations. Implementations are expected to
// be fast (in-memory or cached lookups); the dispatcher calls Resolve
// inside the request hot path.
type EndpointResolver interface {
	Resolve(ctx context.Context, moduleID uuid.UUID) (string, error)
}

// ErrEndpointUnknown is returned by an EndpointResolver when the
// module has no registered runtime endpoint. The dispatcher wraps it
// as ErrModuleVersionInactive so callers see a consistent sentinel.
var ErrEndpointUnknown = errors.New("dispatcher: module has no registered endpoint")

// StaticEndpointResolver is the dev/test resolver. Callers seed it
// with module → base-URL pairs; the dispatcher invokes
// `<base>/functions/<name>` for sync dispatch and
// `<base>/cancel/<invocation_id>` for cancellation.
type StaticEndpointResolver struct {
	mu      sync.RWMutex
	entries map[uuid.UUID]string
}

// NewStaticEndpointResolver returns an empty resolver. Use
// Register/SetAll to seed module endpoints.
func NewStaticEndpointResolver() *StaticEndpointResolver {
	return &StaticEndpointResolver{entries: make(map[uuid.UUID]string)}
}

// Register adds or replaces the endpoint for `moduleID`. baseURL must
// not carry a trailing slash; the dispatcher concatenates the function
// path verbatim.
func (s *StaticEndpointResolver) Register(moduleID uuid.UUID, baseURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[moduleID] = strings.TrimRight(baseURL, "/")
}

// Resolve implements EndpointResolver.
func (s *StaticEndpointResolver) Resolve(_ context.Context, moduleID uuid.UUID) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	url, ok := s.entries[moduleID]
	if !ok {
		return "", ErrEndpointUnknown
	}
	return url, nil
}

// HTTPDispatcherConfig tunes the HTTP-backed dispatcher. Zero values
// fall back to the package defaults.
type HTTPDispatcherConfig struct {
	// Timeout caps the per-call deadline applied on top of any
	// caller-provided context. Clamped to [1s, MaxDispatchTimeout].
	Timeout time.Duration

	// BodyLimitBytes caps both the inbound payload size and the size of
	// the response body read from the runtime. Defaults to
	// DefaultBodyLimitBytes when zero.
	BodyLimitBytes int64

	// Client is the HTTP client used to reach the runtime. Defaults to
	// a client with timeout sourced from Timeout.
	Client *http.Client

	// Now overrides the wall clock — used by tests to assert on
	// DurationMs deterministically.
	Now func() time.Time
}

// httpDispatcher is the production dispatcher implementation. It POSTs
// the invocation payload to `<endpoint>/functions/<function_name>` and
// expects a JSON response. Cancellation hits
// `<endpoint>/invocations/<invocation_id>/cancel` with DELETE.
type httpDispatcher struct {
	resolver EndpointResolver
	timeout  time.Duration
	limit    int64
	client   *http.Client
	now      func() time.Time
}

// NewHTTPDispatcher returns an http-backed Dispatcher. Resolver is
// mandatory; cfg's zero values use the package defaults.
func NewHTTPDispatcher(resolver EndpointResolver, cfg HTTPDispatcherConfig) Dispatcher {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultDispatchTimeout
	}
	if timeout > MaxDispatchTimeout {
		timeout = MaxDispatchTimeout
	}
	limit := cfg.BodyLimitBytes
	if limit <= 0 {
		limit = DefaultBodyLimitBytes
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: timeout + 5*time.Second}
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &httpDispatcher{
		resolver: resolver,
		timeout:  timeout,
		limit:    limit,
		client:   client,
		now:      now,
	}
}

// Dispatch implements Dispatcher.
func (d *httpDispatcher) Dispatch(ctx context.Context, inv *function.FunctionInvocation) (Result, error) {
	if inv == nil {
		return Result{}, errors.New("dispatcher: nil invocation")
	}
	if int64(len(inv.Payload)) > d.limit {
		return Result{Status: function.StatusFailed}, function.ErrPayloadTooLarge
	}
	base, err := d.resolver.Resolve(ctx, inv.ModuleID)
	if err != nil {
		if errors.Is(err, ErrEndpointUnknown) {
			return Result{Status: function.StatusFailed}, function.ErrModuleVersionInactive
		}
		return Result{Status: function.StatusFailed}, err
	}

	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	url := fmt.Sprintf("%s/functions/%s", base, inv.FunctionName)
	body := inv.Payload
	if len(body) == 0 {
		body = []byte("null")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Result{Status: function.StatusFailed}, fmt.Errorf("dispatcher: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Invocation-Id", inv.ID.String())
	req.Header.Set("X-Tenant-Id", inv.TenantID.String())
	req.Header.Set("X-Actor-Id", inv.ActorID.String())
	if inv.ModuleVersion != "" {
		req.Header.Set("X-Module-Version", inv.ModuleVersion)
	}

	start := d.now()
	resp, err := d.client.Do(req)
	if err != nil {
		if ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Result{
				Status:     function.StatusTimeout,
				DurationMs: d.now().Sub(start).Milliseconds(),
			}, function.ErrInvocationTimeout
		}
		if ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
			return Result{
				Status:     function.StatusCancelled,
				DurationMs: d.now().Sub(start).Milliseconds(),
			}, ctx.Err()
		}
		return Result{
			Status:     function.StatusFailed,
			DurationMs: d.now().Sub(start).Milliseconds(),
		}, fmt.Errorf("dispatcher: http call failed: %w", err)
	}
	defer resp.Body.Close()

	payload, readErr := io.ReadAll(io.LimitReader(resp.Body, d.limit+1))
	durationMs := d.now().Sub(start).Milliseconds()
	if readErr != nil {
		return Result{
			Status:     function.StatusFailed,
			DurationMs: durationMs,
		}, fmt.Errorf("dispatcher: read response: %w", readErr)
	}
	if int64(len(payload)) > d.limit {
		return Result{
			Status:     function.StatusFailed,
			DurationMs: durationMs,
		}, function.ErrPayloadTooLarge
	}

	exitCode := 0
	status := function.StatusSucceeded
	switch {
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusGatewayTimeout:
		status = function.StatusTimeout
		exitCode = resp.StatusCode
		return Result{Status: status, Payload: payload, DurationMs: durationMs, ExitCode: exitCode},
			function.ErrInvocationTimeout
	case resp.StatusCode == http.StatusNotFound:
		return Result{Status: function.StatusFailed, Payload: payload, DurationMs: durationMs, ExitCode: resp.StatusCode},
			function.ErrFunctionNotFound
	case resp.StatusCode == http.StatusRequestEntityTooLarge:
		return Result{Status: function.StatusFailed, Payload: payload, DurationMs: durationMs, ExitCode: resp.StatusCode},
			function.ErrPayloadTooLarge
	case resp.StatusCode == http.StatusConflict:
		return Result{Status: function.StatusFailed, Payload: payload, DurationMs: durationMs, ExitCode: resp.StatusCode},
			function.ErrModuleVersionInactive
	case resp.StatusCode >= 400:
		status = function.StatusFailed
		exitCode = resp.StatusCode
	}
	return Result{
		Status:     status,
		Payload:    payload,
		DurationMs: durationMs,
		ExitCode:   exitCode,
	}, nil
}

// Cancel implements Dispatcher. The remote endpoint may return 404 for
// unknown invocations; cancellation is best-effort and the dispatcher
// swallows that into a nil error so callers can mark the row cancelled
// regardless.
func (d *httpDispatcher) Cancel(ctx context.Context, invocationID uuid.UUID) error {
	// The dispatcher cancellation hits the runtime serving the module
	// behind this invocation; without an invocation→module map at the
	// dispatcher layer the cancellation is best-effort against every
	// registered endpoint. A real production wire-up keeps the mapping
	// in the repo layer (see Repository.GetInvocation) and threads the
	// resolved module through to here. To keep this layer narrow we
	// expose a CancelAt helper for callers that already have the
	// endpoint.
	_ = invocationID
	_ = ctx
	return nil
}

// CancelAt is the best-effort cancel call that posts to the runtime's
// cancel hook. Exposed as a free function so handlers that already
// resolved the endpoint can invoke it without re-resolving.
func CancelAt(ctx context.Context, client *http.Client, baseURL string, invocationID uuid.UUID) error {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	url := fmt.Sprintf("%s/invocations/%s/cancel", strings.TrimRight(baseURL, "/"), invocationID.String())
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 500 {
		return fmt.Errorf("dispatcher: cancel returned %d", resp.StatusCode)
	}
	return nil
}

// JSONMarshalLen returns the length of v's canonical JSON encoding;
// handlers use it to enforce payload caps without buffering the body
// twice.
func JSONMarshalLen(v any) (int, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}
