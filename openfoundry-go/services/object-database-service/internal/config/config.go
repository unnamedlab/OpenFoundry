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
	CassandraContactPoints string
	CassandraLocalDC       string
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "object-database-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50125)
	cfg.CassandraContactPoints = os.Getenv("CASSANDRA_CONTACT_POINTS")
	cfg.CassandraLocalDC = defaultStr(os.Getenv("CASSANDRA_LOCAL_DC"), "dc1")
	return cfg, nil
}

// CassandraPoints splits the comma-separated contact-points env var.
func (c *Config) CassandraPoints() []string {
	if strings.TrimSpace(c.CassandraContactPoints) == "" {
		return nil
	}
	parts := strings.Split(c.CassandraContactPoints, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
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
