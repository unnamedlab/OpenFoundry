package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/openfoundry/openfoundry-go/services/ontology-actions-service/internal/config"
)

func TestPythonSidecarConfigMapsServiceConfigToManager(t *testing.T) {
	cfg := &config.Config{
		PythonSidecarBinary:  "/opt/openfoundry-pyruntime",
		PythonSidecarArgs:    []string{"--debug"},
		PythonSidecarEnv:     []string{"PYRUNTIME_LOG=debug"},
		PythonSidecarTimeout: 11 * time.Second,
	}
	got := pythonSidecarConfig(cfg)
	if got.BinaryPath != cfg.PythonSidecarBinary {
		t.Fatalf("BinaryPath = %q", got.BinaryPath)
	}
	if !reflect.DeepEqual(got.Args, cfg.PythonSidecarArgs) {
		t.Fatalf("Args = %#v", got.Args)
	}
	if !reflect.DeepEqual(got.Env, cfg.PythonSidecarEnv) {
		t.Fatalf("Env = %#v", got.Env)
	}
	if got.StartupTimeout != cfg.PythonSidecarTimeout || got.HardCallTimeout != cfg.PythonSidecarTimeout {
		t.Fatalf("timeouts = startup %s hard %s", got.StartupTimeout, got.HardCallTimeout)
	}

	got.Args[0] = "mutated"
	got.Env[0] = "mutated=1"
	if cfg.PythonSidecarArgs[0] == "mutated" || cfg.PythonSidecarEnv[0] == "mutated=1" {
		t.Fatalf("pythonSidecarConfig must defensively copy slices")
	}
}
