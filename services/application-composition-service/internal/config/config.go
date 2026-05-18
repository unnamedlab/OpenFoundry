// Package config resolves application-composition-service env config.
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
	DatabaseURL  string
	JWTSecret    string
	MetricsAddr  string
	WarehouseURI string

	// Composition-specific auth and tenant knobs.
	JWTIssuer            string
	JWTAudience          string
	DefaultTokenTTLSecs  int64
	LongLivedTokenTTLSec int64
	OAuthIntegrationURL  string
	DefaultTenant        string
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "application-composition-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50140)
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return nil, &MissingEnvError{Key: "DATABASE_URL"}
	}
	cfg.JWTSecret = defaultStr(os.Getenv("OPENFOUNDRY_JWT_SECRET"), os.Getenv("JWT_SECRET"))
	if cfg.JWTSecret == "" {
		return nil, &MissingEnvError{Key: "JWT_SECRET"}
	}
	cfg.WarehouseURI = defaultStr(os.Getenv("ICEBERG_CATALOG_WAREHOUSE_URI"), "s3://openfoundry-warehouse")
	cfg.MetricsAddr = defaultStr(os.Getenv("METRICS_ADDR"), "0.0.0.0:9090")
	cfg.JWTIssuer = defaultStr(os.Getenv("ICEBERG_JWT_ISSUER"), "foundry-iceberg")
	cfg.JWTAudience = defaultStr(os.Getenv("ICEBERG_JWT_AUDIENCE"), "iceberg-catalog")
	cfg.DefaultTokenTTLSecs = parseInt64(os.Getenv("ICEBERG_DEFAULT_TOKEN_TTL_SECS"), 3600)
	cfg.LongLivedTokenTTLSec = parseInt64(os.Getenv("ICEBERG_LONG_LIVED_TOKEN_TTL_SECS"), 90*24*3600)
	cfg.OAuthIntegrationURL = defaultStr(os.Getenv("OAUTH_INTEGRATION_URL"), "http://oauth-integration-service:8080")
	cfg.DefaultTenant = defaultStr(os.Getenv("ICEBERG_DEFAULT_TENANT"), "default")
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

func parseInt64(v string, fallback int64) int64 {
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}
