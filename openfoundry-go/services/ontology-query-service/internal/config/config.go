// Package config resolves ontology-query-service env config.
//
// Cassandra-first: the service reads from Cassandra/Scylla; no Postgres
// pool is required (per the Rust S1.5.e note, sqlx::migrate is not
// applied — schema lives in Cassandra).
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Service struct{ Name, Version string }
	Server  struct {
		Host string
		Port uint16
	}
	JWTSecret              string
	CassandraContactPoints string
	CassandraKeyspace      string
	NATSURL                string
	MetricsAddr            string
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "ontology-query-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50123)
	cfg.JWTSecret = defaultStr(os.Getenv("OPENFOUNDRY_JWT_SECRET"), os.Getenv("JWT_SECRET"))
	if cfg.JWTSecret == "" {
		return nil, &MissingEnvError{Key: "JWT_SECRET"}
	}
	cfg.CassandraContactPoints = os.Getenv("CASSANDRA_CONTACT_POINTS")
	cfg.CassandraKeyspace = defaultStr(os.Getenv("CASSANDRA_KEYSPACE"), "ontology")
	cfg.NATSURL = os.Getenv("NATS_URL")
	cfg.MetricsAddr = defaultStr(os.Getenv("METRICS_ADDR"), "0.0.0.0:9090")
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
