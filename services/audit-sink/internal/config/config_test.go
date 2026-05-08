package config

import (
	"errors"
	"testing"
)

func TestFromEnvRequiresIcebergCatalogForProductionWriter(t *testing.T) {
	t.Setenv("KAFKA_BOOTSTRAP_SERVERS", "localhost:9092")
	t.Setenv("AUDIT_SINK_JSONL_PATH", "")
	t.Setenv("ICEBERG_CATALOG_URL", "")

	_, err := FromEnv()
	if !errors.As(err, new(*MissingEnvError)) {
		t.Fatalf("FromEnv() error = %v, want MissingEnvError", err)
	}
	var missing *MissingEnvError
	if !errors.As(err, &missing) || missing.Key != "ICEBERG_CATALOG_URL" {
		t.Fatalf("FromEnv() missing = %#v, want ICEBERG_CATALOG_URL", missing)
	}
}

func TestFromEnvAllowsExplicitJSONLDevModeWithoutIcebergCatalog(t *testing.T) {
	t.Setenv("KAFKA_BOOTSTRAP_SERVERS", "localhost:9092")
	t.Setenv("AUDIT_SINK_JSONL_PATH", "-")
	t.Setenv("ICEBERG_CATALOG_URL", "")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.JSONLWriterPath != "-" {
		t.Fatalf("JSONLWriterPath = %q, want '-'", cfg.JSONLWriterPath)
	}
	if cfg.CatalogURL != "" {
		t.Fatalf("CatalogURL = %q, want empty in JSONL dev mode", cfg.CatalogURL)
	}
}

func TestFromEnvSelectsIcebergProductionModeWhenJSONLUnset(t *testing.T) {
	t.Setenv("KAFKA_BOOTSTRAP_SERVERS", "localhost:9092")
	t.Setenv("AUDIT_SINK_JSONL_PATH", "")
	t.Setenv("ICEBERG_CATALOG_URL", "http://lakekeeper:8181")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv() error = %v", err)
	}
	if cfg.JSONLWriterPath != "" {
		t.Fatalf("JSONLWriterPath = %q, want empty for Iceberg mode", cfg.JSONLWriterPath)
	}
	if cfg.CatalogURL != "http://lakekeeper:8181" {
		t.Fatalf("CatalogURL = %q", cfg.CatalogURL)
	}
}
