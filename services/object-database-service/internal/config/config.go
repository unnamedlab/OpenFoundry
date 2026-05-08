// Package config resolves object-database-service env config.
package config

import (
	"os"
	"strconv"
	"strings"
)

// BackendMode discriminates the storage backend selected at boot.
// Mirrors Rust `BackendMode` (snake_case JSON: "cassandra" | "in_memory").
type BackendMode string

const (
	BackendCassandra BackendMode = "cassandra"
	BackendInMemory  BackendMode = "in_memory"
)

type Config struct {
	Service struct{ Name, Version string }
	Server  struct {
		Host string
		Port uint16
	}
	// DevMode is the only startup mode that permits in-memory storage. It is
	// intended for unit tests, smoke tests and explicit local development.
	DevMode bool

	Backend BackendMode

	CassandraContactPoints  string
	CassandraObjectKeyspace string
	CassandraLinkKeyspace   string
	CassandraUsername       string
	CassandraPassword       string
	CassandraLocalDC        string
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "object-database-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50125)
	cfg.DevMode = parseBool(os.Getenv("OF_DEV_STUB_MODE")) || parseBool(os.Getenv("ALLOW_SUBSTRATE_STUBS"))
	cfg.Backend = parseBackendMode(os.Getenv("OBJECT_DATABASE_BACKEND"), BackendCassandra)
	cfg.CassandraContactPoints = os.Getenv("CASSANDRA_CONTACT_POINTS")
	cfg.CassandraObjectKeyspace = firstNonEmpty(os.Getenv("CASSANDRA_OBJECT_KEYSPACE"), os.Getenv("CASSANDRA_KEYSPACE"), "ontology_objects")
	cfg.CassandraLinkKeyspace = firstNonEmpty(os.Getenv("CASSANDRA_LINK_KEYSPACE"), os.Getenv("CASSANDRA_KEYSPACE"), "ontology_indexes")
	cfg.CassandraUsername = os.Getenv("CASSANDRA_USERNAME")
	cfg.CassandraPassword = os.Getenv("CASSANDRA_PASSWORD")
	cfg.CassandraLocalDC = defaultStr(os.Getenv("CASSANDRA_LOCAL_DC"), "dc1")
	return cfg, nil
}

// CassandraPoints splits the comma-separated contact-points env var.
func (c *Config) CassandraPoints() []string {
	return splitCSV(c.CassandraContactPoints)
}

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

func parseBackendMode(v string, fallback BackendMode) BackendMode {
	switch BackendMode(strings.ToLower(strings.TrimSpace(v))) {
	case BackendCassandra:
		return BackendCassandra
	case BackendInMemory:
		return BackendInMemory
	default:
		return fallback
	}
}

func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
