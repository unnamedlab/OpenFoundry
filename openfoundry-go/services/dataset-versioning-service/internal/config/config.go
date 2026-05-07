// Package config resolves dataset-versioning-service env config.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Service struct{ Name, Version string }
	Server  struct {
		Host string
		Port uint16
	}
	DatabaseURL string
	JWTSecret   string
	MetricsAddr string
	// RetentionWorkerEnabled controls the in-process branch-archive
	// worker (Foundry "Branch retention" doc). Off by default — operators
	// turn it on per environment. Mirrors the Rust feature toggle.
	RetentionWorkerEnabled bool
	// RetentionWorkerInterval is the tick cadence of the worker. Defaults
	// to one hour to match Rust.
	RetentionWorkerInterval time.Duration
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "dataset-versioning-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50117)
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return nil, &MissingEnvError{Key: "DATABASE_URL"}
	}
	cfg.JWTSecret = defaultStr(os.Getenv("OPENFOUNDRY_JWT_SECRET"), os.Getenv("JWT_SECRET"))
	if cfg.JWTSecret == "" {
		return nil, &MissingEnvError{Key: "JWT_SECRET"}
	}
	cfg.MetricsAddr = defaultStr(os.Getenv("METRICS_ADDR"), "0.0.0.0:9090")
	cfg.RetentionWorkerEnabled = parseBool(os.Getenv("RETENTION_WORKER_ENABLED"), false)
	cfg.RetentionWorkerInterval = parseDuration(os.Getenv("RETENTION_WORKER_INTERVAL"), time.Hour)
	return cfg, nil
}

func parseBool(v string, fallback bool) bool {
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func parseDuration(v string, fallback time.Duration) time.Duration {
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
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
