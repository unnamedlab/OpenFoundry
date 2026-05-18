package config

import "testing"

func TestFromEnvReadsPythonSidecarConfig(t *testing.T) {
	t.Setenv("PYTHON_SIDECAR_BINARY", "/tmp/openfoundry-pyruntime")
	t.Setenv("PYTHON_SIDECAR_TIMEOUT_SECONDS", "17")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.PythonSidecarBinary != "/tmp/openfoundry-pyruntime" {
		t.Fatalf("PythonSidecarBinary = %q", cfg.PythonSidecarBinary)
	}
	if cfg.PythonSidecarTimeoutSeconds != 17 {
		t.Fatalf("PythonSidecarTimeoutSeconds = %d, want 17", cfg.PythonSidecarTimeoutSeconds)
	}
}

func TestFromEnvDefaultsPythonSidecarTimeout(t *testing.T) {
	t.Setenv("PYTHON_SIDECAR_TIMEOUT_SECONDS", "not-a-number")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.PythonSidecarTimeoutSeconds != 60 {
		t.Fatalf("PythonSidecarTimeoutSeconds = %d, want default 60", cfg.PythonSidecarTimeoutSeconds)
	}
}

func TestFromEnvReadsRequiredPythonSidecarFlag(t *testing.T) {
	t.Setenv("OF_PIPELINE_TRANSFORMS_REQUIRED", "true")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if !cfg.RequirePythonSidecar {
		t.Fatal("RequirePythonSidecar = false, want true")
	}
}
