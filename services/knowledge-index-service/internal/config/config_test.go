package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadDatabaseURLEnvOverride(t *testing.T) {
	t.Setenv("OF_DATABASE__URL", "postgres://of-db")
	t.Setenv("DATABASE_URL", "postgres://fallback-db")

	cfg, err := Load(writeConfig(t, `service:
  name: knowledge
server: {}
jwt: {}
telemetry: {}
database:
  url: ""
allow_fake_store: false
`), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.URL != "postgres://of-db" {
		t.Fatalf("database url = %q", cfg.Database.URL)
	}
}

func TestLoadDatabaseURLFallback(t *testing.T) {
	t.Setenv("OF_DATABASE__URL", "")
	t.Setenv("DATABASE_URL", "postgres://fallback-db")

	cfg, err := Load(writeConfig(t, `service:
  name: knowledge
server: {}
jwt: {}
telemetry: {}
database:
  url: ""
allow_fake_store: true
`), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.URL != "postgres://fallback-db" {
		t.Fatalf("database url = %q", cfg.Database.URL)
	}
	if !cfg.AllowFakeStore {
		t.Fatal("allow_fake_store should unmarshal from yaml")
	}
}

func TestLoadFlatDatabaseURLAlias(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	cfg, err := Load(writeConfig(t, `service:
  name: knowledge
server: {}
jwt: {}
telemetry: {}
database_url: postgres://flat-db
allow_fake_store: false
`), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.URL != "postgres://flat-db" {
		t.Fatalf("database url = %q", cfg.Database.URL)
	}
}
