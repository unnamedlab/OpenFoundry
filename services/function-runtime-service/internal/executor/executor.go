// Package executor runs user-authored function code under a timeout
// and (where the kernel supports it) a memory limit.
//
// v0 ships direct process executors:
//
//   - TS: invokes `node <tempfile>` reading the function body from the
//     SourceURI when it points at a local file (file:// or relative
//     path). Stdout is treated as the JSON result.
//   - Python: invokes `python3 <tempfile>` with the same contract.
//
// Both process executors are deliberately small. v1 will replace them with a
// real isolated runtime (deno or v8go for TS; subinterpreter or wasm
// for Python). See README.md in this package for the trade-off log.
//
// Sentinels surface via internal/domain so the HTTP layer can map
// timeouts → 504 vs failures → 500.
package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
)

// Sentinel signalling the runtime binary (node / python3) is not
// installed. Surfaced separately so the HTTP layer can return 503
// instead of conflating it with a user-side execution failure.
var ErrRuntimeUnavailable = errors.New("executor: runtime unavailable in this environment")

// ErrOutputLimitExceeded signals that user code emitted more stdout/stderr than
// the configured capture ceiling. The process is terminated when possible.
var ErrOutputLimitExceeded = errors.New("executor: output limit exceeded")

// Result is everything an executor returns when a run finishes.
//
// Output is the raw stdout payload (callers parse it as JSON when the
// signature demands it); Stderr is captured for the audit / debug
// view. ExitCode is 0 on success.
type Result struct {
	Output   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
}

// Executor runs one function version against a JSON input.
//
// Implementations MUST honour ctx cancellation and the timeout the
// handler clamps against the configured ceiling. On timeout they MUST
// return domain.ErrExecutionTimeout wrapped via fmt.Errorf so callers
// can errors.Is against the sentinel.
type Executor interface {
	Execute(ctx context.Context, fn models.FunctionDefinition, version models.FunctionVersion, input []byte) (*Result, error)
}

// Limits controls the hard ceilings every executor enforces.
type Limits struct {
	Timeout        time.Duration
	MemoryLimitMiB uint64
	MaxStdoutBytes uint64
	MaxStderrBytes uint64

	// AllowRemoteSourceURI is intentionally false by default. Remote source
	// schemes still need a vetted fetcher; enabling the flag without one keeps
	// failing closed.
	AllowRemoteSourceURI bool
}

// Registry dispatches to a runtime-specific Executor based on
// fn.Runtime. Lookup is keyed by models.Runtime.
type Registry struct {
	byRuntime map[models.Runtime]Executor
	enabled   map[models.Runtime]bool
}

// NewRegistry builds an empty registry. Register one executor per
// supported runtime.
func NewRegistry() *Registry {
	return &Registry{byRuntime: map[models.Runtime]Executor{}, enabled: map[models.Runtime]bool{}}
}

// Register binds rt to ex and marks the runtime enabled.
func (r *Registry) Register(rt models.Runtime, ex Executor) {
	r.byRuntime[rt] = ex
	r.enabled[rt] = true
}

// RegisterUnavailable marks rt enabled but currently unavailable (for example,
// because its binary is absent in a non-production environment).
func (r *Registry) RegisterUnavailable(rt models.Runtime, err error) {
	r.byRuntime[rt] = unavailableExecutor{err: err}
	r.enabled[rt] = true
}

// Enabled reports whether rt is configured for this service instance.
func (r *Registry) Enabled(rt models.Runtime) bool { return r.enabled[rt] }

// RuntimeStatus is a safe health/capability view of one runtime.
type RuntimeStatus struct {
	Runtime   models.Runtime `json:"runtime"`
	Enabled   bool           `json:"enabled"`
	Available bool           `json:"available"`
	Error     string         `json:"error,omitempty"`
}

func (r *Registry) RuntimeStatuses(ctx context.Context) []RuntimeStatus {
	out := make([]RuntimeStatus, 0, len(r.enabled))
	for rt := range r.enabled {
		st := RuntimeStatus{Runtime: rt, Enabled: true, Available: true}
		if ex, ok := r.byRuntime[rt].(interface{ Availability(context.Context) error }); ok {
			if err := ex.Availability(ctx); err != nil {
				st.Available = false
				st.Error = err.Error()
			}
		}
		out = append(out, st)
	}
	return out
}

// Execute dispatches based on fn.Runtime. Returns ErrExecutorNotAvailable
// when the runtime has no registered executor.
func (r *Registry) Execute(ctx context.Context, fn models.FunctionDefinition, version models.FunctionVersion, input []byte) (*Result, error) {
	ex, ok := r.byRuntime[fn.Runtime]
	if !ok || !r.enabled[fn.Runtime] {
		return nil, domain.ErrExecutorNotAvailable
	}
	return ex.Execute(ctx, fn, version, input)
}

type unavailableExecutor struct{ err error }

func (u unavailableExecutor) Execute(context.Context, models.FunctionDefinition, models.FunctionVersion, []byte) (*Result, error) {
	if u.err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRuntimeUnavailable, u.err)
	}
	return nil, ErrRuntimeUnavailable
}

func (u unavailableExecutor) Availability(context.Context) error {
	if u.err != nil {
		return u.err
	}
	return ErrRuntimeUnavailable
}

// ─── Shared process helper ──────────────────────────────────────────────

// runScript is the shared process driver. It:
//   - materialises sourceURI into a temp file (or copies a file:/// path).
//   - applies an OS-level rlimit hook (linux build tag).
//   - enforces a timeout via context.WithTimeout.
//   - feeds input as JSON on stdin so the script can `process.stdin`
//     / `sys.stdin.read()` without crafting argv quoting rules.
//
// The caller supplies the binary path and any leading args (typically
// just the script path appended after) — we keep the env minimal so
// authored code does not inherit host secrets.
func runScript(ctx context.Context, bin string, sourceURI string, input []byte, lim Limits) (*Result, error) {
	if bin == "" {
		return nil, fmt.Errorf("%w: empty binary path", ErrRuntimeUnavailable)
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("%w: %s not in $PATH (%v) — replace this runtime binary before enabling this runtime", ErrRuntimeUnavailable, bin, err)
	}

	scriptPath, cleanup, err := materialise(sourceURI, lim.AllowRemoteSourceURI)
	if err != nil {
		return nil, fmt.Errorf("materialise source: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}

	timeout := lim.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(runCtx, bin, scriptPath)
	cmd.Dir = filepath.Dir(scriptPath)
	cmd.Env = []string{
		"PATH=/usr/bin:/bin:/usr/local/bin",
		"LANG=C.UTF-8",
		"OF_FUNCTION_RUNTIME=1",
	}
	if len(input) > 0 {
		cmd.Stdin = strings.NewReader(string(input))
	} else {
		cmd.Stdin = strings.NewReader("{}")
	}
	applyRlimit(cmd, lim) // build-tag-gated; no-op on darwin/windows

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", bin, err)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	stdoutCh := make(chan streamResult, 1)
	stderrCh := make(chan streamResult, 1)
	go func() {
		defer wg.Done()
		res := readBounded(stdout, limitOrDefault(lim.MaxStdoutBytes))
		if res.err != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		stdoutCh <- res
	}()
	go func() {
		defer wg.Done()
		res := readBounded(stderr, limitOrDefault(lim.MaxStderrBytes))
		if res.err != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		stderrCh <- res
	}()
	wg.Wait()
	waitErr := cmd.Wait()
	close(stdoutCh)
	close(stderrCh)
	outRes := <-stdoutCh
	errRes := <-stderrCh
	dur := time.Since(start)

	res := &Result{
		Output:   outRes.data,
		Stderr:   errRes.data,
		ExitCode: 0,
		Duration: dur,
	}
	if outRes.err != nil || errRes.err != nil {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		streamName := "stdout"
		streamErr := outRes.err
		if streamErr == nil {
			streamName = "stderr"
			streamErr = errRes.err
		}
		return res, fmt.Errorf("%w: %s exceeded capture limit: %v", ErrOutputLimitExceeded, streamName, streamErr)
	}
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			res.ExitCode = ee.ExitCode()
		}
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return res, fmt.Errorf("%w (after %s)", domain.ErrExecutionTimeout, dur.Round(time.Millisecond))
		}
		return res, fmt.Errorf("%w: %v", domain.ErrExecutionFailed, waitErr)
	}
	return res, nil
}

// materialise resolves sourceURI to a real on-disk script path the
// runtime binary can execute. Supported shapes:
//
//   - `file://abs/path` — used as-is (no copy, no cleanup).
//   - `/abs/path` or `./relative` — used as-is.
//   - `inline:<body>` — written to a temp file (cleanup deletes it).
//
// Any other scheme returns ErrRuntimeUnavailable; v1 will wire fetches
// against code-repository-service blobs.
func materialise(sourceURI string, allowRemote bool) (string, func(), error) {
	if sourceURI == "" {
		return "", nil, fmt.Errorf("%w: empty source_uri", domain.ErrInvalidArgument)
	}
	if strings.HasPrefix(sourceURI, "inline:") {
		body := strings.TrimPrefix(sourceURI, "inline:")
		dir, err := os.MkdirTemp("", "of-fn-*")
		if err != nil {
			return "", nil, err
		}
		path := filepath.Join(dir, "function.tmp")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			_ = os.RemoveAll(dir)
			return "", nil, err
		}
		return path, func() { _ = os.RemoveAll(dir) }, nil
	}
	if strings.HasPrefix(sourceURI, "file://") {
		p := strings.TrimPrefix(sourceURI, "file://")
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if _, err := os.Stat(p); err != nil {
			return "", nil, fmt.Errorf("stat %s: %w", p, err)
		}
		return p, nil, nil
	}
	u, err := url.Parse(sourceURI)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "code-repo") {
		if !allowRemote {
			return "", nil, fmt.Errorf("%w: remote source_uri scheme %q is disabled", domain.ErrInvalidArgument, u.Scheme)
		}
		return "", nil, fmt.Errorf("%w: remote source_uri scheme %q has no configured fetcher", ErrRuntimeUnavailable, u.Scheme)
	}
	// Treat as a local path.
	if _, err := os.Stat(sourceURI); err != nil {
		return "", nil, fmt.Errorf("stat %s: %w", sourceURI, err)
	}
	return sourceURI, nil, nil
}

type streamResult struct {
	data []byte
	err  error
}

func limitOrDefault(limit uint64) uint64 {
	if limit == 0 {
		return 1 << 20
	}
	return limit
}

func readBounded(r io.Reader, limit uint64) streamResult {
	var buf bytes.Buffer
	limited := io.LimitReader(r, int64(limit)+1)
	_, err := io.Copy(&buf, limited)
	if err != nil {
		return streamResult{data: buf.Bytes(), err: err}
	}
	if uint64(buf.Len()) > limit {
		data := buf.Bytes()[:limit]
		return streamResult{data: data, err: fmt.Errorf("%w: limit=%d", ErrOutputLimitExceeded, limit)}
	}
	return streamResult{data: buf.Bytes()}
}
