// Package config resolves llm-catalog-service env config.
//
// The Rust binary is `fn main(){}` with handlers/models/domain re-
// exported from libs/ai-kernel via `#[path = "..."]`. The Go port
// becomes the canonical implementation by consuming
// libs/ai-kernel-go/models for the wire-format DTO surface.
//
// Foundation slice scope: substrate-only HTTP. The `/api/v1/*`
// routes (provider CRUD, list, etc.) wire in a follow-up slice
// alongside libs/ai-kernel-go/handlers + libs/ai-kernel-go/domain/llm.
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
	DatabaseURL                   string
	JWTSecret                     string
	CheckpointsPurposeServiceURL  string
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "llm-catalog-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50095)
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	cfg.JWTSecret = os.Getenv("JWT_SECRET")
	cfg.CheckpointsPurposeServiceURL = defaultStr(os.Getenv("CHECKPOINTS_PURPOSE_SERVICE_URL"), "http://localhost:50116")
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
