package pythonsidecar

import (
	"os"
	"reflect"
	"testing"
	"time"
)

func TestConfigNormalizePreservesArgsEnvAndAppliesTimeoutDefaults(t *testing.T) {
	bin := t.TempDir() + "/sidecar"
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write bin: %v", err)
	}
	cfg := Config{BinaryPath: bin, Args: []string{"--debug"}, Env: []string{"PYRUNTIME_LOG=debug"}}
	if err := cfg.normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if !reflect.DeepEqual(cfg.Args, []string{"--debug"}) || !reflect.DeepEqual(cfg.Env, []string{"PYRUNTIME_LOG=debug"}) {
		t.Fatalf("args/env drift: %#v %#v", cfg.Args, cfg.Env)
	}
	if cfg.StartupTimeout != 10*time.Second || cfg.HardCallTimeout != 60*time.Second {
		t.Fatalf("timeouts = startup %s hard %s", cfg.StartupTimeout, cfg.HardCallTimeout)
	}
}
