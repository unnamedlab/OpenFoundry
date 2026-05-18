package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/config"
)

func TestBuildRuntimeRegistry_ProductionFailsWhenEnabledBinaryMissing(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Environment = "production"
	cfg.Executor.EnabledRuntimes = []string{"ts"}
	cfg.Executor.NodeBinary = "/non/existent/node-for-startup-test"
	cfg.Database.AllowMemoryStore = false

	_, _, err := buildRuntimeRegistry(cfg)
	if err == nil {
		t.Fatal("expected production startup to fail for missing enabled runtime binary")
	}
}

func TestBuildStore_ProductionFailsWhenDatabaseMissingUnlessExplicitlyAllowed(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Environment = "production"
	cfg.Database.URL = ""
	cfg.Database.AllowMemoryStore = false

	_, _, _, err := buildStore(context.Background(), cfg, slog.Default())
	if err == nil {
		t.Fatal("expected production startup to fail without database.url")
	}
}

func TestBuildStore_ProductionAllowsMemoryStoreWhenExplicitlyAllowed(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	cfg.Service.Environment = "production"
	cfg.Database.URL = ""
	cfg.Database.AllowMemoryStore = true

	store, _, closeFn, err := buildStore(context.Background(), cfg, slog.Default())
	if err != nil {
		t.Fatalf("buildStore: %v", err)
	}
	if store == nil {
		t.Fatal("expected in-memory store")
	}
	if closeFn != nil {
		t.Fatal("memory store should not have close function")
	}
}
