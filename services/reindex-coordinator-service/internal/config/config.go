// Package config resolves reindex-coordinator-service env config.
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
	DatabaseURL              string
	DatabaseMaxConnections   int32
	KafkaBootstrap           string
	CassandraContactPoints   string
	CassandraKeyspace        string
	OpenLineageNamespace     string
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "reindex-coordinator-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(defaultStr(os.Getenv("PORT"), os.Getenv("METRICS_ADDR")), 9090)
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	cfg.DatabaseMaxConnections = parseInt32(os.Getenv("DATABASE_MAX_CONNECTIONS"), 10)
	cfg.KafkaBootstrap = os.Getenv("KAFKA_BOOTSTRAP_SERVERS")
	cfg.CassandraContactPoints = os.Getenv("CASSANDRA_CONTACT_POINTS")
	cfg.CassandraKeyspace = defaultStr(os.Getenv("CASSANDRA_KEYSPACE"), "ontology_objects")
	cfg.OpenLineageNamespace = defaultStr(os.Getenv("OF_OPENLINEAGE_NAMESPACE"), "openfoundry")

	if cfg.DatabaseURL == "" {
		return nil, &MissingEnvError{Key: "DATABASE_URL"}
	}
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

func parseInt32(v string, fallback int32) int32 {
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return fallback
	}
	return int32(n)
}
