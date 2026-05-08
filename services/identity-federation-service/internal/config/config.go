// Package config resolves identity-federation-service env config (slice 1).
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config bundles slice-1 settings.
//
// Future slices add: NATS_URL, REDIS_URL, KAFKA_BOOTSTRAP_SERVERS,
// VAULT_ADDR, ICEBERG_*, CASSANDRA_*. Documented in the inventory.
type Config struct {
	Service struct{ Name, Version string }
	Server  struct {
		Host string
		Port uint16
	}
	DatabaseURL string
	JWTSecret   string

	AccessTTL  time.Duration
	RefreshTTL time.Duration

	MetricsAddr string
}

// FromEnv resolves config. Required: DATABASE_URL, JWT_SECRET (or
// OPENFOUNDRY_JWT_SECRET).
func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "identity-federation-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50112)

	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return nil, &MissingEnvError{Key: "DATABASE_URL"}
	}
	cfg.JWTSecret = defaultStr(os.Getenv("OPENFOUNDRY_JWT_SECRET"), os.Getenv("JWT_SECRET"))
	if cfg.JWTSecret == "" {
		return nil, &MissingEnvError{Key: "JWT_SECRET"}
	}

	cfg.AccessTTL = parseDur(os.Getenv("ACCESS_TOKEN_TTL"), time.Hour)
	cfg.RefreshTTL = parseDur(os.Getenv("REFRESH_TOKEN_TTL"), 7*24*time.Hour)
	cfg.MetricsAddr = defaultStr(os.Getenv("METRICS_ADDR"), "0.0.0.0:9090")
	return cfg, nil
}

// MissingEnvError signals a required env var was unset.
type MissingEnvError struct{ Key string }

func (e *MissingEnvError) Error() string {
	return fmt.Sprintf("required environment variable %s is not set", e.Key)
}

// IsMissingEnv reports whether err is a MissingEnvError.
func IsMissingEnv(err error) bool {
	var me *MissingEnvError
	return errors.As(err, &me)
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

func parseDur(v string, fallback time.Duration) time.Duration {
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
