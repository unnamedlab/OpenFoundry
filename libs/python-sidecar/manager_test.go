package pythonsidecar

import (
	"os"
	"strings"
	"testing"
)

func TestSidecarBinaryUnsetIsSkipped(t *testing.T) {
	t.Setenv("PYTHON_SIDECAR_BINARY", "")
	if os.Getenv("PYTHON_SIDECAR_BINARY") != "" {
		t.Fatal("PYTHON_SIDECAR_BINARY should be unset in this test")
	}
	_, err := New(Config{BinaryPath: ""}, nil)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "binarypath") || !strings.Contains(strings.ToLower(err.Error()), "required") {
		t.Fatalf("expected missing binary path validation, got %v", err)
	}
}
