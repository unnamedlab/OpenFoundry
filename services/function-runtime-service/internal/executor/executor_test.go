package executor_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/executor"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
)

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

func TestTSProcessExecutor_MissingBinary(t *testing.T) {
	t.Parallel()
	ex := executor.NewTSProcessExecutor("/non/existent/node-binary-for-test", executor.Limits{Timeout: time.Second})
	_, err := ex.Execute(context.Background(),
		models.FunctionDefinition{Runtime: models.RuntimeTypeScript},
		models.FunctionVersion{SourceURI: "inline:success"},
		nil)
	if !errors.Is(err, executor.ErrRuntimeUnavailable) {
		t.Fatalf("expected ErrRuntimeUnavailable, got %v", err)
	}
}

func TestProcessExecutor_FakeBinarySuccess(t *testing.T) {
	t.Parallel()
	bin := buildFakeRuntime(t)
	ex := executor.NewTSProcessExecutor(bin, executor.Limits{Timeout: 5 * time.Second})
	res, err := ex.Execute(context.Background(),
		models.FunctionDefinition{Runtime: models.RuntimeTypeScript},
		models.FunctionVersion{SourceURI: "inline:success"},
		[]byte(`{"hello":"world"}`))
	if err != nil {
		t.Fatalf("execute: %v (stderr=%s)", err, res.Stderr)
	}
	if string(res.Output) != `{"ok":true}` {
		t.Fatalf("unexpected output: %q", res.Output)
	}
}

func TestProcessExecutor_FakeBinaryCapturesStderrAndExitCode(t *testing.T) {
	t.Parallel()
	bin := buildFakeRuntime(t)
	ex := executor.NewTSProcessExecutor(bin, executor.Limits{Timeout: 5 * time.Second})
	res, err := ex.Execute(context.Background(),
		models.FunctionDefinition{Runtime: models.RuntimeTypeScript},
		models.FunctionVersion{SourceURI: "inline:stderr-exit"},
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

func TestProcessExecutor_Timeout(t *testing.T) {
	t.Parallel()
	bin := buildFakeRuntime(t)
	ex := executor.NewTSProcessExecutor(bin, executor.Limits{Timeout: 150 * time.Millisecond})
	_, err := ex.Execute(context.Background(),
		models.FunctionDefinition{Runtime: models.RuntimeTypeScript},
		models.FunctionVersion{SourceURI: "inline:timeout"},
		nil)
	if !errors.Is(err, domain.ErrExecutionTimeout) {
		t.Fatalf("expected ErrExecutionTimeout, got %v", err)
	}
}

func TestProcessExecutor_StdoutLimit(t *testing.T) {
	t.Parallel()
	bin := buildFakeRuntime(t)
	ex := executor.NewTSProcessExecutor(bin, executor.Limits{Timeout: 5 * time.Second, MaxStdoutBytes: 8, MaxStderrBytes: 1024})
	res, err := ex.Execute(context.Background(),
		models.FunctionDefinition{Runtime: models.RuntimeTypeScript},
		models.FunctionVersion{SourceURI: "inline:stdout-big"},
		nil)
	if !errors.Is(err, executor.ErrOutputLimitExceeded) {
		t.Fatalf("expected ErrOutputLimitExceeded, got %v", err)
	}
	if len(res.Output) != 8 {
		t.Fatalf("expected truncated stdout length 8, got %d", len(res.Output))
	}
}

func TestProcessExecutor_StderrLimit(t *testing.T) {
	t.Parallel()
	bin := buildFakeRuntime(t)
	ex := executor.NewTSProcessExecutor(bin, executor.Limits{Timeout: 5 * time.Second, MaxStdoutBytes: 1024, MaxStderrBytes: 8})
	res, err := ex.Execute(context.Background(),
		models.FunctionDefinition{Runtime: models.RuntimeTypeScript},
		models.FunctionVersion{SourceURI: "inline:stderr-big"},
		nil)
	if !errors.Is(err, executor.ErrOutputLimitExceeded) {
		t.Fatalf("expected ErrOutputLimitExceeded, got %v", err)
	}
	if len(res.Stderr) != 8 {
		t.Fatalf("expected truncated stderr length 8, got %d", len(res.Stderr))
	}
}

func TestProcessExecutor_DoesNotInheritHostSecretEnv(t *testing.T) {
	t.Setenv("SECRET_TEST_TOKEN", "super-secret")
	bin := buildFakeRuntime(t)
	ex := executor.NewTSProcessExecutor(bin, executor.Limits{Timeout: 5 * time.Second})
	res, err := ex.Execute(context.Background(),
		models.FunctionDefinition{Runtime: models.RuntimeTypeScript},
		models.FunctionVersion{SourceURI: "inline:check-env"},
		nil)
	if err != nil {
		t.Fatalf("execute: %v (stderr=%s)", err, res.Stderr)
	}
	if strings.Contains(string(res.Output), "super-secret") || string(res.Output) != `{"secret_present":false}` {
		t.Fatalf("secret env leaked into output: %q", res.Output)
	}
}

func TestMaterialise_RejectsRemoteSourceWhenDisabled(t *testing.T) {
	t.Parallel()
	bin := buildFakeRuntime(t)
	ex := executor.NewTSProcessExecutor(bin, executor.Limits{Timeout: time.Second})
	_, err := ex.Execute(context.Background(),
		models.FunctionDefinition{Runtime: models.RuntimeTypeScript},
		models.FunctionVersion{SourceURI: "http://example.com/script.js"},
		nil)
	if !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument for disabled remote source, got %v", err)
	}
}

func buildFakeRuntime(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "fake_runtime.go")
	binary := filepath.Join(dir, "fake-runtime")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	code := `package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(2)
	}
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(2)
	}
	switch strings.TrimSpace(string(b)) {
	case "success":
		_, _ = io.ReadAll(os.Stdin)
		fmt.Print("{\"ok\":true}")
	case "stderr-exit":
		fmt.Fprint(os.Stderr, "boom")
		os.Exit(3)
	case "timeout":
		time.Sleep(10 * time.Second)
	case "stdout-big":
		fmt.Print(strings.Repeat("x", 4096))
	case "stderr-big":
		fmt.Fprint(os.Stderr, strings.Repeat("e", 4096))
	case "check-env":
		if os.Getenv("SECRET_TEST_TOKEN") != "" {
			fmt.Print("{\"secret_present\":true}")
			return
		}
		fmt.Print("{\"secret_present\":false}")
	default:
		fmt.Print("{}")
	}
}
`
	if err := os.WriteFile(source, []byte(code), 0o600); err != nil {
		t.Fatalf("write fake runtime: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", binary, source)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fake runtime: %v\n%s", err, out)
	}
	return binary
}

// ─── fakes ────────────────────────────────────────────────────────────

type fakeExecutor struct{ name string }

func (f fakeExecutor) Execute(_ context.Context, _ models.FunctionDefinition, _ models.FunctionVersion, _ []byte) (*executor.Result, error) {
	return &executor.Result{Output: []byte(f.name)}, nil
}
