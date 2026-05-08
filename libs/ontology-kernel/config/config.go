// Package config is the Go port of `libs/ontology-kernel/src/config.rs`.
//
// AppConfig is the environment-driven configuration consumed by every
// ontology-* binary. The Rust source uses the `config` crate with
// `Environment::default().separator("__")` so nested fields (none today)
// would be flattened with `__` as the path separator. Today every field
// is flat — the table reads scalar env vars by their UPPER_SNAKE name.
//
// Defaults match the Rust helper functions byte-for-byte; tests pin
// each one.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// AppConfig mirrors `struct AppConfig`.
type AppConfig struct {
	Host                          string
	Port                          uint16
	DatabaseURL                   string
	JWTSecret                     string
	AuditServiceURL               string
	DatasetServiceURL             string
	OntologyServiceURL            string
	PipelineServiceURL            string
	AIServiceURL                  string
	SearchEmbeddingProvider       string
	NotificationServiceURL        string
	NodeRuntimeCommand            string
	ConnectorManagementServiceURL string
}

// Defaults mirror `default_*()` helpers in config.rs.
const (
	DefaultHost                          = "0.0.0.0"
	DefaultPort                          = uint16(50057)
	DefaultAuditServiceURL               = "http://localhost:50070"
	DefaultDatasetServiceURL             = "http://localhost:50079"
	DefaultOntologyServiceURL            = "http://localhost:50057"
	DefaultPipelineServiceURL            = "http://localhost:50081"
	DefaultAIServiceURL                  = "http://localhost:50127"
	DefaultSearchEmbeddingProvider       = "deterministic-hash"
	DefaultNotificationServiceURL        = "http://localhost:50114"
	DefaultNodeRuntimeCommand            = "node"
	DefaultConnectorManagementServiceURL = "http://localhost:50130"
)

// Default returns a zero-environment AppConfig with every defaulted
// field populated and the two required fields (DatabaseURL, JWTSecret)
// left empty. [FromEnv] then layers env values on top.
func Default() AppConfig {
	return AppConfig{
		Host:                          DefaultHost,
		Port:                          DefaultPort,
		AuditServiceURL:               DefaultAuditServiceURL,
		DatasetServiceURL:             DefaultDatasetServiceURL,
		OntologyServiceURL:            DefaultOntologyServiceURL,
		PipelineServiceURL:            DefaultPipelineServiceURL,
		AIServiceURL:                  DefaultAIServiceURL,
		SearchEmbeddingProvider:       DefaultSearchEmbeddingProvider,
		NotificationServiceURL:        DefaultNotificationServiceURL,
		NodeRuntimeCommand:            DefaultNodeRuntimeCommand,
		ConnectorManagementServiceURL: DefaultConnectorManagementServiceURL,
	}
}

// FromEnv mirrors `AppConfig::from_env()`. Reads the same env vars the
// Rust `config` crate would surface (UPPER_SNAKE names, no prefix).
// Required fields without a default (DatabaseURL, JWTSecret) return an
// error if absent — matching `try_deserialize` rejecting a missing
// non-Option field.
func FromEnv() (AppConfig, error) {
	return FromGetenv(os.Getenv)
}

// FromGetenv is the testable inner that takes any `func(key) string`
// resolver. Rust's `config` crate also accepts arbitrary providers
// (env, file, etc.); we mirror that injection point.
func FromGetenv(get func(string) string) (AppConfig, error) {
	c := Default()

	pickString := func(key string, dst *string) {
		if v := get(key); v != "" {
			*dst = v
		}
	}
	pickString("HOST", &c.Host)
	pickString("DATABASE_URL", &c.DatabaseURL)
	pickString("JWT_SECRET", &c.JWTSecret)
	pickString("AUDIT_SERVICE_URL", &c.AuditServiceURL)
	pickString("DATASET_SERVICE_URL", &c.DatasetServiceURL)
	pickString("ONTOLOGY_SERVICE_URL", &c.OntologyServiceURL)
	pickString("PIPELINE_SERVICE_URL", &c.PipelineServiceURL)
	pickString("AI_SERVICE_URL", &c.AIServiceURL)
	pickString("SEARCH_EMBEDDING_PROVIDER", &c.SearchEmbeddingProvider)
	pickString("NOTIFICATION_SERVICE_URL", &c.NotificationServiceURL)
	pickString("NODE_RUNTIME_COMMAND", &c.NodeRuntimeCommand)
	pickString("CONNECTOR_MANAGEMENT_SERVICE_URL", &c.ConnectorManagementServiceURL)

	if v := strings.TrimSpace(get("PORT")); v != "" {
		n, err := strconv.ParseUint(v, 10, 16)
		if err != nil {
			return AppConfig{}, fmt.Errorf("PORT: %w", err)
		}
		c.Port = uint16(n)
	}

	if c.DatabaseURL == "" {
		return AppConfig{}, fmt.Errorf("missing required env var: DATABASE_URL")
	}
	if c.JWTSecret == "" {
		return AppConfig{}, fmt.Errorf("missing required env var: JWT_SECRET")
	}
	return c, nil
}
