// Package config resolves ai-evaluation-service env config.
//
// The Rust binary is `fn main(){}` and the evaluation handlers
// (benchmark_providers + evaluate_guardrails) cross-reference
// libs/ai-kernel domain types (provider, evaluation, llm/{gateway,
// guardrails, runtime}). The Go port is substrate-only until the
// libs/ai-kernel-go domain/llm slice lands.
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
	cfg.Service.Name = "ai-evaluation-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50075)
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
