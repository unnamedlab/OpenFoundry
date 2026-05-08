package config

import "testing"

func TestFromEnvDatabaseURLAbsentSmokeModeFalseByDefault(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("NOTEBOOK_RUNTIME_SMOKE_MODE", "")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.DatabaseURL != "" {
		t.Fatalf("DatabaseURL = %q, want unset", cfg.DatabaseURL)
	}
	if cfg.SmokeMode {
		t.Fatal("SmokeMode = true, want false when NOTEBOOK_RUNTIME_SMOKE_MODE is unset")
	}
}

func TestFromEnvSmokeModeTrueAllowsNoDatabaseSmokeCRUD(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("NOTEBOOK_RUNTIME_SMOKE_MODE", "true")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.DatabaseURL != "" {
		t.Fatalf("DatabaseURL = %q, want unset", cfg.DatabaseURL)
	}
	if !cfg.SmokeMode {
		t.Fatal("SmokeMode = false, want true when NOTEBOOK_RUNTIME_SMOKE_MODE=true")
	}
}

func TestFromEnvPythonSidecarBinaryAbsentIsExplicitlyUnset(t *testing.T) {
	t.Setenv("PYTHON_SIDECAR_BINARY", "")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.PythonSidecarBinary != "" {
		t.Fatalf("PythonSidecarBinary = %q, want unset", cfg.PythonSidecarBinary)
	}
}
