// Package config mirrors `services/ontology-actions-service/src/config.rs`.
//
// Field-for-field 1:1 with the Rust struct so the existing Helm
// envSecrets templates and runbook overrides resolve identically.
// Defaults match ports allocated in `services/edge-gateway-service`.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	Service struct {
		Name    string
		Version string
	}

	Server struct {
		Host string
		Port uint16
	}

	DatabaseURL string
	JWTSecret   string

	AuditServiceURL                string
	DatasetServiceURL              string
	OntologyServiceURL             string
	PipelineServiceURL             string
	AIServiceURL                   string
	NotificationServiceURL         string
	SearchEmbeddingProvider        string
	NodeRuntimeCommand             string
	ConnectorManagementServiceURL  string

	CassandraContactPoints string
	CassandraLocalDC       string

	// PythonSidecarBinary is the absolute path to the openfoundry-pyruntime
	// executable. When empty, inline Python functions return
	// ErrPythonRuntimeNotWired (legacy behaviour).
	PythonSidecarBinary string
}

func FromEnv() (*Config, error) {
	c := &Config{}
	c.Service.Name = "ontology-actions-service"
	c.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	c.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	c.Server.Port = parseUint16(os.Getenv("PORT"), 50106)
	c.DatabaseURL = os.Getenv("DATABASE_URL")
	c.JWTSecret = os.Getenv("JWT_SECRET")
	c.AuditServiceURL = defaultStr(os.Getenv("AUDIT_SERVICE_URL"), "http://localhost:50115")
	c.DatasetServiceURL = defaultStr(os.Getenv("DATASET_SERVICE_URL"), "http://localhost:50079")
	c.OntologyServiceURL = defaultStr(os.Getenv("ONTOLOGY_SERVICE_URL"), "http://localhost:50103")
	c.PipelineServiceURL = defaultStr(os.Getenv("PIPELINE_SERVICE_URL"), "http://localhost:50081")
	c.AIServiceURL = defaultStr(os.Getenv("AI_SERVICE_URL"), "http://localhost:50127")
	c.NotificationServiceURL = defaultStr(os.Getenv("NOTIFICATION_SERVICE_URL"), "http://localhost:50114")
	c.SearchEmbeddingProvider = defaultStr(os.Getenv("SEARCH_EMBEDDING_PROVIDER"), "deterministic-hash")
	c.NodeRuntimeCommand = defaultStr(os.Getenv("NODE_RUNTIME_COMMAND"), "node")
	c.ConnectorManagementServiceURL = defaultStr(os.Getenv("CONNECTOR_MANAGEMENT_SERVICE_URL"), "http://localhost:50130")
	c.CassandraContactPoints = os.Getenv("CASSANDRA_CONTACT_POINTS")
	c.CassandraLocalDC = defaultStr(os.Getenv("CASSANDRA_LOCAL_DC"), "dc1")
	c.PythonSidecarBinary = os.Getenv("PYTHON_SIDECAR_BIN")
	return c, nil
}

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
