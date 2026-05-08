// Package config resolves retrieval-context-service env config.
//
// The Rust binary is `fn main(){}` and every public surface
// (handlers/models/domain) is a `#[path]` re-export from
// libs/ai-kernel. The document-intelligence sub-domain is gated
// behind a `parsers` Cargo feature with a doc-comment noting that
// wiring AppState + a router is intentionally out of scope for the
// consolidation PR. Substrate-only port; routes wire alongside
// libs/ai-kernel-go/handlers in a follow-up slice.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	Service struct{ Name, Version string }
	Server  struct {
		Host string
		Port uint16
	}
	DatabaseURL string
	JWTSecret   string
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "retrieval-context-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50098)
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	cfg.JWTSecret = os.Getenv("JWT_SECRET")
	return cfg, nil
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
