package config

import (
	"reflect"
	"testing"
	"time"
)

func TestFromEnvPythonSidecarCanonicalConfig(t *testing.T) {
	t.Setenv("PYTHON_SIDECAR_BINARY", "/opt/openfoundry-pyruntime")
	t.Setenv("PYTHON_SIDECAR_BIN", "/legacy/ignored")
	t.Setenv("PYTHON_SIDECAR_ARGS", "--log-level debug --feature-x")
	t.Setenv("PYTHON_SIDECAR_ENV", "PYRUNTIME_LOG=debug,PATH=/venv/bin\nCUSTOM=1")
	t.Setenv("PYTHON_SIDECAR_TIMEOUT", "23s")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.PythonSidecarBinary != "/opt/openfoundry-pyruntime" {
		t.Fatalf("binary = %q", cfg.PythonSidecarBinary)
	}
	if !reflect.DeepEqual(cfg.PythonSidecarArgs, []string{"--log-level", "debug", "--feature-x"}) {
		t.Fatalf("args = %#v", cfg.PythonSidecarArgs)
	}
	if !reflect.DeepEqual(cfg.PythonSidecarEnv, []string{"PYRUNTIME_LOG=debug", "PATH=/venv/bin", "CUSTOM=1"}) {
		t.Fatalf("env = %#v", cfg.PythonSidecarEnv)
	}
	if cfg.PythonSidecarTimeout != 23*time.Second {
		t.Fatalf("timeout = %s", cfg.PythonSidecarTimeout)
	}
}

func TestFromEnvPythonSidecarLegacyBinaryAliasAndSecondsTimeout(t *testing.T) {
	t.Setenv("PYTHON_SIDECAR_BIN", "/legacy/openfoundry-pyruntime")
	t.Setenv("PYTHON_SIDECAR_TIMEOUT", "7")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.PythonSidecarBinary != "/legacy/openfoundry-pyruntime" {
		t.Fatalf("binary alias = %q", cfg.PythonSidecarBinary)
	}
	if cfg.PythonSidecarTimeout != 7*time.Second {
		t.Fatalf("timeout = %s", cfg.PythonSidecarTimeout)
	}
}
