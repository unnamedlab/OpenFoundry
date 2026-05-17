package config

import (
	"errors"
	"strings"
	"testing"
)

func TestFromEnv_happy(t *testing.T) {
	t.Setenv("KAFKA_BOOTSTRAP_SERVERS", "kafka-1:9092,kafka-2:9092")
	t.Setenv("ICEBERG_CATALOG_URL", "http://catalog.example")
	t.Setenv("ICEBERG_WAREHOUSE", "openfoundry")
	t.Setenv("ACTION_LOG_SINK_BATCH_MAX_RECORDS", "5000")
	t.Setenv("ACTION_LOG_SINK_BATCH_MAX_WAIT_SECONDS", "10")

	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.Service.Name != "action-log-sink" {
		t.Errorf("Service.Name = %q", cfg.Service.Name)
	}
	if cfg.CatalogURL != "http://catalog.example" {
		t.Errorf("CatalogURL = %q", cfg.CatalogURL)
	}
	if cfg.TableWriterURL != cfg.CatalogURL {
		t.Errorf("TableWriterURL should default to CatalogURL when unset, got %q", cfg.TableWriterURL)
	}
	if cfg.Warehouse != "openfoundry" {
		t.Errorf("Warehouse = %q", cfg.Warehouse)
	}
	if cfg.BatchPolicy.MaxRecords != 5000 {
		t.Errorf("BatchPolicy.MaxRecords = %d", cfg.BatchPolicy.MaxRecords)
	}
	if cfg.BatchPolicy.MaxWait.Seconds() != 10 {
		t.Errorf("BatchPolicy.MaxWait = %v", cfg.BatchPolicy.MaxWait)
	}
	if cfg.MetricsAddr != "0.0.0.0:9090" {
		t.Errorf("MetricsAddr default = %q", cfg.MetricsAddr)
	}
}

func TestFromEnv_missingBootstrap(t *testing.T) {
	t.Setenv("KAFKA_BOOTSTRAP_SERVERS", "")
	_, err := FromEnv()
	if !IsMissingEnv(err) {
		t.Fatalf("expected MissingEnvError, got %v", err)
	}
	var me *MissingEnvError
	if errors.As(err, &me) && me.Key != "KAFKA_BOOTSTRAP_SERVERS" {
		t.Errorf("missing key = %q", me.Key)
	}
}

func TestFromEnv_missingCatalogURLWhenNoJSONL(t *testing.T) {
	t.Setenv("KAFKA_BOOTSTRAP_SERVERS", "kafka:9092")
	t.Setenv("ICEBERG_CATALOG_URL", "")
	t.Setenv("ACTION_LOG_SINK_JSONL_PATH", "")
	_, err := FromEnv()
	var me *MissingEnvError
	if !errors.As(err, &me) || me.Key != "ICEBERG_CATALOG_URL" {
		t.Fatalf("expected missing ICEBERG_CATALOG_URL, got %v", err)
	}
}

func TestFromEnv_jsonlSkipsCatalogRequirement(t *testing.T) {
	t.Setenv("KAFKA_BOOTSTRAP_SERVERS", "kafka:9092")
	t.Setenv("ICEBERG_CATALOG_URL", "")
	t.Setenv("ACTION_LOG_SINK_JSONL_PATH", "/tmp/out.jsonl")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.JSONLWriterPath != "/tmp/out.jsonl" {
		t.Errorf("JSONLWriterPath = %q", cfg.JSONLWriterPath)
	}
}

func TestFromEnv_invalidBatchMaxRecords(t *testing.T) {
	t.Setenv("KAFKA_BOOTSTRAP_SERVERS", "kafka:9092")
	t.Setenv("ICEBERG_CATALOG_URL", "http://c.example")
	t.Setenv("ACTION_LOG_SINK_BATCH_MAX_RECORDS", "-5")
	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected invalid env error")
	}
	if !strings.Contains(err.Error(), "ACTION_LOG_SINK_BATCH_MAX_RECORDS") {
		t.Errorf("error should name the key: %v", err)
	}
}

func TestBatchPolicy_ShouldFlush(t *testing.T) {
	t.Parallel()
	p := BatchPolicy{MaxRecords: 10, MaxWait: 5}
	if p.ShouldFlush(5, 1) {
		t.Error("should not flush below both thresholds")
	}
	if !p.ShouldFlush(10, 0) {
		t.Error("should flush at MaxRecords")
	}
	if !p.ShouldFlush(0, 5) {
		t.Error("should flush at MaxWait")
	}
}
