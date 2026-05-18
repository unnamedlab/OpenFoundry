package executor_test

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/executor"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
)

func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func TestRegistry_DispatchesByRuntime(t *testing.T) {
	t.Parallel()
	reg := executor.NewRegistry()
	reg.Register(models.RuntimeTypeScript, fakeExecutor{name: "ts"})
	reg.Register(models.RuntimePython, fakeExecutor{name: "py"})

	for _, tc := range []struct {
		rt   models.Runtime
		want string
	}{
		{models.RuntimeTypeScript, "ts"},
		{models.RuntimePython, "py"},
	} {
		tc := tc
		t.Run(string(tc.rt), func(t *testing.T) {
			t.Parallel()
			res, err := reg.Execute(context.Background(),
				models.FunctionDefinition{Runtime: tc.rt},
				models.FunctionVersion{},
				nil)
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if string(res.Output) != tc.want {
				t.Fatalf("dispatched to wrong runtime: got %q want %q", res.Output, tc.want)
			}
		})
	}

	if _, err := reg.Execute(context.Background(),
		models.FunctionDefinition{Runtime: "rust"},
		models.FunctionVersion{},
		nil); !errors.Is(err, domain.ErrExecutorNotAvailable) {
		t.Fatalf("expected ErrExecutorNotAvailable, got %v", err)
	}
}

func TestTSStubExecutor_MissingBinary(t *testing.T) {
	t.Parallel()
	ex := executor.NewTSStubExecutor("/non/existent/node-binary-for-test", executor.Limits{Timeout: time.Second})
	_, err := ex.Execute(context.Background(),
		models.FunctionDefinition{Runtime: models.RuntimeTypeScript},
		models.FunctionVersion{SourceURI: "inline:console.log('{}')"},
		nil)
	if !errors.Is(err, executor.ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented, got %v", err)
	}
}

// TestTSStubExecutor_RoundTrip is skipped when `node` is not on $PATH
// so the test suite stays green on minimal CI images.
func TestTSStubExecutor_RoundTrip(t *testing.T) {
	t.Parallel()
	if !hasBinary("node") {
		t.Skip("node binary unavailable; skipping live runtime test")
	}
	body := `let data=''; process.stdin.on('data', c=>data+=c); process.stdin.on('end', ()=>{process.stdout.write(JSON.stringify({echo: JSON.parse(data)}))})`
	ex := executor.NewTSStubExecutor("", executor.Limits{Timeout: 5 * time.Second})
	res, err := ex.Execute(context.Background(),
		models.FunctionDefinition{Runtime: models.RuntimeTypeScript},
		models.FunctionVersion{SourceURI: "inline:" + body},
		[]byte(`{"hello":"world"}`))
	if err != nil {
		t.Fatalf("execute: %v (stderr=%s)", err, res.Stderr)
	}
	if !strings.Contains(string(res.Output), `"hello":"world"`) {
		t.Fatalf("unexpected output: %s", res.Output)
	}
}

func TestTSStubExecutor_Timeout(t *testing.T) {
	t.Parallel()
	if !hasBinary("node") {
		t.Skip("node binary unavailable; skipping live runtime test")
	}
	// `while(true);` busy loop. Wrap in a 150ms timeout — runScript
	// must return ErrExecutionTimeout, NOT ErrExecutionFailed.
	body := `while (true) {}`
	ex := executor.NewTSStubExecutor("", executor.Limits{Timeout: 150 * time.Millisecond})
	_, err := ex.Execute(context.Background(),
		models.FunctionDefinition{Runtime: models.RuntimeTypeScript},
		models.FunctionVersion{SourceURI: "inline:" + body},
		nil)
	if !errors.Is(err, domain.ErrExecutionTimeout) {
		t.Fatalf("expected ErrExecutionTimeout, got %v", err)
	}
}

func TestTSStubExecutor_CapturesStderrAndExitCode(t *testing.T) {
	t.Parallel()
	if !hasBinary("node") {
		t.Skip("node binary unavailable; skipping live runtime test")
	}
	body := `process.stderr.write('boom'); process.exit(3)`
	ex := executor.NewTSStubExecutor("", executor.Limits{Timeout: 5 * time.Second})
	res, err := ex.Execute(context.Background(),
		models.FunctionDefinition{Runtime: models.RuntimeTypeScript},
		models.FunctionVersion{SourceURI: "inline:" + body},
		nil)
	if !errors.Is(err, domain.ErrExecutionFailed) {
		t.Fatalf("expected ErrExecutionFailed, got %v", err)
	}
	if res == nil || res.ExitCode != 3 {
		t.Fatalf("expected exit code 3, got %+v", res)
	}
	if !strings.Contains(string(res.Stderr), "boom") {
		t.Fatalf("expected stderr to contain 'boom', got %q", res.Stderr)
	}
}

func TestMaterialise_RejectsUnknownScheme(t *testing.T) {
	t.Parallel()
	// Drive the failure path through TS executor (any executor works).
	ex := executor.NewTSStubExecutor("/bin/echo", executor.Limits{Timeout: time.Second}) // /bin/echo always present
	_, err := ex.Execute(context.Background(),
		models.FunctionDefinition{Runtime: models.RuntimeTypeScript},
		models.FunctionVersion{SourceURI: "http://example.com/script.js"},
		nil)
	if !errors.Is(err, executor.ErrNotImplemented) {
		t.Fatalf("expected ErrNotImplemented for http source, got %v", err)
	}
}

// ─── fakes ────────────────────────────────────────────────────────────

type fakeExecutor struct{ name string }

func (f fakeExecutor) Execute(_ context.Context, _ models.FunctionDefinition, _ models.FunctionVersion, _ []byte) (*executor.Result, error) {
	return &executor.Result{Output: []byte(f.name)}, nil
}
