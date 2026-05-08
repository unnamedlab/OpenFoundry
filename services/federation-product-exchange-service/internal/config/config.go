// Package config resolves federation-product-exchange-service env config.
//
// Post-S8 ownership boundary (ADR-0030 / B21): this binary absorbs
// the legacy marketplace + marketplace-catalog + product-distribution
// services. The Rust binary is `fn main(){}` with the three sub-domains
// held as `#[allow(dead_code)]` modules until the consolidated main
// is wired; the Go port is canonical (same pattern as authz-policy /
// entity-resolution).
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
	DatabaseURL            string
	MarketplaceDatabaseURL string
	JWTSecret              string
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "federation-product-exchange-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	// Rust default port is 50120 but Go ingestion-replication-service
	// already claims that allocation; the Go port uses 50126 (next
	// free in the openfoundry-go allocation table). Override via PORT
	// when running side-by-side with the Rust binary.
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50126)
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	cfg.MarketplaceDatabaseURL = os.Getenv("MARKETPLACE_DATABASE_URL")
	cfg.JWTSecret = os.Getenv("JWT_SECRET")
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
