// Package config resolves lineage-service env config.
//
// The Rust binary supports two runtime modes selected via
// LINEAGE_RUNTIME_MODE: kafka_to_iceberg vs http_health.
// Foundation slice ports the http_health mode; the Kafka→Iceberg
// runtime lands in a follow-up slice.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// RuntimeMode discriminates the binary's runtime.
type RuntimeMode string

const (
	ModeKafkaToIceberg RuntimeMode = "kafka_to_iceberg"
	ModeHTTPHealth     RuntimeMode = "http_health"
)

// RuntimeModeFromEnv mirrors the Rust selection logic:
//   - explicit LINEAGE_RUNTIME_MODE=kafka|kafka_to_iceberg|iceberg → KafkaToIceberg
//   - explicit LINEAGE_RUNTIME_MODE=http|http_health → HTTPHealth
//   - both ICEBERG_CATALOG_URL + KAFKA_BOOTSTRAP_SERVERS set → KafkaToIceberg
//   - otherwise → HTTPHealth (foundation default)
func RuntimeModeFromEnv() RuntimeMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LINEAGE_RUNTIME_MODE"))) {
	case "kafka", "kafka_to_iceberg", "iceberg":
		return ModeKafkaToIceberg
	case "http", "http_health":
		return ModeHTTPHealth
	}
	if os.Getenv("ICEBERG_CATALOG_URL") != "" && os.Getenv("KAFKA_BOOTSTRAP_SERVERS") != "" {
		return ModeKafkaToIceberg
	}
	return ModeHTTPHealth
}

type Config struct {
	Service struct{ Name, Version string }
	Server  struct {
		Host string
		Port uint16
	}
	DatabaseURL                       string
	JWTSecret                         string
	DataDir                           string
	DatasetServiceURL                 string
	WorkflowServiceURL                string
	AIServiceURL                      string
	StorageBackend                    string
	StorageBucket                     string
	S3Endpoint                        string
	S3Region                          string
	S3AccessKey                       string
	S3SecretKey                       string
	LocalStorageRoot                  string
	DistributedPipelineWorkers        uint32
	DistributedComputePollIntervalMs  uint64
	DistributedComputeTimeoutSecs     uint64
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "lineage-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50083)
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	cfg.JWTSecret = os.Getenv("JWT_SECRET")
	cfg.DataDir = defaultStr(os.Getenv("DATA_DIR"), "/tmp/pipeline-data")
	cfg.DatasetServiceURL = defaultStr(os.Getenv("DATASET_SERVICE_URL"), "http://localhost:50079")
	cfg.WorkflowServiceURL = defaultStr(os.Getenv("WORKFLOW_SERVICE_URL"), "http://localhost:50137")
	cfg.AIServiceURL = defaultStr(os.Getenv("AI_SERVICE_URL"), "http://localhost:50127")
	cfg.StorageBackend = defaultStr(os.Getenv("STORAGE_BACKEND"), "s3")
	cfg.StorageBucket = defaultStr(os.Getenv("STORAGE_BUCKET"), "datasets")
	cfg.S3Endpoint = os.Getenv("S3_ENDPOINT")
	cfg.S3Region = os.Getenv("S3_REGION")
	cfg.S3AccessKey = os.Getenv("S3_ACCESS_KEY")
	cfg.S3SecretKey = os.Getenv("S3_SECRET_KEY")
	cfg.LocalStorageRoot = os.Getenv("LOCAL_STORAGE_ROOT")
	cfg.DistributedPipelineWorkers = parseUint32(os.Getenv("DISTRIBUTED_PIPELINE_WORKERS"), 1)
	cfg.DistributedComputePollIntervalMs = parseUint64(os.Getenv("DISTRIBUTED_COMPUTE_POLL_INTERVAL_MS"), 5000)
	cfg.DistributedComputeTimeoutSecs = parseUint64(os.Getenv("DISTRIBUTED_COMPUTE_TIMEOUT_SECS"), 900)

	// Foundation slice runs in HTTP-health mode. DATABASE_URL +
	// JWT_SECRET are not required there (they ARE required by the
	// follow-up runtime slice that owns the query handlers).
	return cfg, nil
}

type MissingEnvError struct{ Key string }

func (e *MissingEnvError) Error() string {
	return fmt.Sprintf("required environment variable %s is not set", e.Key)
}

func IsMissingEnv(err error) bool { var me *MissingEnvError; return errors.As(err, &me) }

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func parseUint16(v string, fallback uint16) uint16 {
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseUint(v, 10, 16)
	if err != nil {
		return fallback
	}
	return uint16(n)
}

func parseUint32(v string, fallback uint32) uint32 {
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return fallback
	}
	return uint32(n)
}

func parseUint64(v string, fallback uint64) uint64 {
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}
