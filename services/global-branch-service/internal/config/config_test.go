package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateProductDatabaseProductionWithoutDBFails(t *testing.T) {
	t.Parallel()

	cfg := &Config{Environment: "production", AllowUnwiredProductRoutes: true}

	err := cfg.ValidateProductDatabase()
	if !errors.Is(err, ErrDatabaseRequired) {
		t.Fatalf("ValidateProductDatabase() error=%v want ErrDatabaseRequired", err)
	}
}

func TestValidateProductDatabaseDevSmokeWithoutDBRequiresExplicitFlag(t *testing.T) {
	t.Parallel()

	cfg := &Config{Environment: "development"}
	if err := cfg.ValidateProductDatabase(); !errors.Is(err, ErrDatabaseRequired) {
		t.Fatalf("ValidateProductDatabase() without smoke flag error=%v want ErrDatabaseRequired", err)
	}

	cfg.AllowUnwiredProductRoutes = true
	if err := cfg.ValidateProductDatabase(); err != nil {
		t.Fatalf("ValidateProductDatabase() with smoke flag error=%v want nil", err)
	}
}

func TestLoadFallsBackToDatabaseURLEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/openfoundry")
	clearOFDatabaseURL(t)

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`
service:
  name: global-branch-service
server:
  addr: ":8080"
environment: development
allow_unwired_product_routes: false
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("Load() error=%v", err)
	}
	if got := cfg.DatabaseURL; got != "postgres://user:pass@localhost:5432/openfoundry" {
		t.Fatalf("DatabaseURL=%q want DATABASE_URL fallback", got)
	}
}

func clearOFDatabaseURL(t *testing.T) {
	t.Helper()
	old, ok := os.LookupEnv("OF_DATABASE_URL")
	if err := os.Unsetenv("OF_DATABASE_URL"); err != nil {
		t.Fatalf("unset OF_DATABASE_URL: %v", err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv("OF_DATABASE_URL", old)
			return
		}
		_ = os.Unsetenv("OF_DATABASE_URL")
	})
}
