// Package executor runs user-authored function code under a timeout
// and (where the kernel supports it) a memory limit.
//
// v0 ships shell-based stubs:
//
//   - TS: invokes `node <tempfile>` reading the function body from the
//     SourceURI when it points at a local file (file:// or relative
//     path). Stdout is treated as the JSON result.
//   - Python: invokes `python3 <tempfile>` with the same contract.
//
// Both stubs are deliberately small. v1 will replace them with a
// real isolated runtime (deno or v8go for TS; subinterpreter or wasm
// for Python). See README.md in this package for the trade-off log.
//
// Sentinels surface via internal/domain so the HTTP layer can map
// timeouts → 504 vs failures → 500.
package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
)

// Sentinel signalling the runtime binary (node / python3) is not
// installed. Surfaced separately so the HTTP layer can return 501
// instead of conflating it with a user-side execution failure.
var ErrNotImplemented = errors.New("executor: runtime not implemented in this environment")

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
}

// Registry dispatches to a runtime-specific Executor based on
// fn.Runtime. Lookup is keyed by models.Runtime.
type Registry struct {
	byRuntime map[models.Runtime]Executor
}

// NewRegistry builds an empty registry. Register one executor per
// supported runtime.
func NewRegistry() *Registry {
	return &Registry{byRuntime: map[models.Runtime]Executor{}}
}

// Register binds rt to ex.
func (r *Registry) Register(rt models.Runtime, ex Executor) { r.byRuntime[rt] = ex }

// Execute dispatches based on fn.Runtime. Returns ErrExecutorNotAvailable
// when the runtime has no registered executor.
func (r *Registry) Execute(ctx context.Context, fn models.FunctionDefinition, version models.FunctionVersion, input []byte) (*Result, error) {
	ex, ok := r.byRuntime[fn.Runtime]
	if !ok {
		return nil, domain.ErrExecutorNotAvailable
	}
	return ex.Execute(ctx, fn, version, input)
}

// ─── Shared shell helper ──────────────────────────────────────────────

// runScript is the shared shell driver. It:
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
		return nil, fmt.Errorf("%w: empty binary path", ErrNotImplemented)
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("%w: %s not in $PATH (%v) — replace this stub with deno/isolated-vm/v8go for v1", ErrNotImplemented, bin, err)
	}

	scriptPath, cleanup, err := materialise(sourceURI)
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
	outBytes, _ := io.ReadAll(stdout)
	errBytes, _ := io.ReadAll(stderr)
	waitErr := cmd.Wait()
	dur := time.Since(start)

	res := &Result{
		Output:   outBytes,
		Stderr:   errBytes,
		ExitCode: 0,
		Duration: dur,
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
// Any other scheme returns ErrNotImplemented; v1 will wire fetches
// against code-repository-service blobs.
func materialise(sourceURI string) (string, func(), error) {
	if sourceURI == "" {
		return "", nil, fmt.Errorf("%w: empty source_uri", domain.ErrInvalidArgument)
	}
	if strings.HasPrefix(sourceURI, "inline:") {
		body := strings.TrimPrefix(sourceURI, "inline:")
		f, err := os.CreateTemp("", "of-fn-*.tmp")
		if err != nil {
			return "", nil, err
		}
		if _, err := f.WriteString(body); err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
			return "", nil, err
		}
		_ = f.Close()
		return f.Name(), func() { _ = os.Remove(f.Name()) }, nil
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
		return "", nil, fmt.Errorf("%w: remote source_uri (%s) — wire code-repository-service fetch in v1", ErrNotImplemented, u.Scheme)
	}
	// Treat as a local path.
	if _, err := os.Stat(sourceURI); err != nil {
		return "", nil, fmt.Errorf("stat %s: %w", sourceURI, err)
	}
	return sourceURI, nil, nil
}
