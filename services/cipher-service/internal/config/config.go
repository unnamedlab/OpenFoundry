// Package config loads the cipher-service configuration via koanf,
// mirroring docs/templates/service-skeleton/internal/config so platform tooling sees
// the same precedence rules.
package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config is the typed view of every knob the cipher-service exposes.
type Config struct {
	Service struct {
		Name    string `koanf:"name"`
		Version string `koanf:"version"`
	} `koanf:"service"`

	Server struct {
		Addr            string `koanf:"addr"`
		ShutdownTimeout string `koanf:"shutdown_timeout"`
	} `koanf:"server"`

	JWT struct {
		Secret   string `koanf:"secret"`
		Issuer   string `koanf:"issuer"`
		Audience string `koanf:"audience"`
	} `koanf:"jwt"`

	Telemetry struct {
		OTLPEndpoint string `koanf:"otlp_endpoint"`
		LogFormat    string `koanf:"log_format"`
	} `koanf:"telemetry"`

	// Database carries the Postgres DSN for the cipher key registry.
	// Override via OF_DATABASE__URL or the DATABASE_URL env var
	// (resolved at Load-time so existing platform-wide env wiring
	// keeps working).
	Database struct {
		URL string `koanf:"url"`
	} `koanf:"database"`

	// KMS picks the backend for wrapping DEKs. "local" reads
	// OF_CIPHER_LOCAL_KEK from the env (dev/test). "aws" returns
	// ErrAWSNotImplemented at runtime — config knob today, real
	// client in Milestone C (CIP.20).
	KMS struct {
		Backend  string `koanf:"backend"`
		AWSKeyARN string `koanf:"aws_key_arn"`
	} `koanf:"kms"`
}

// Load resolves the configuration following the documented precedence:
// baked defaults < image config.yaml < CONFIG_FILE override < OF_ env.
func Load(defaultsPath, envPath string) (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(file.Provider(defaultsPath), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("load defaults from %s: %w", defaultsPath, err)
	}
	if envPath != "" {
		if err := k.Load(file.Provider(envPath), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("load env config from %s: %w", envPath, err)
		}
	}
	envProvider := env.Provider("OF_", ".", func(s string) string {
		return strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(s, "OF_"), "__", "."))
	})
	if err := k.Load(envProvider, nil); err != nil {
		return nil, fmt.Errorf("load env overrides: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	// Honour the platform-wide DATABASE_URL convention so the
	// service drops into the same dev stack as identity-federation
	// and tenancy-organizations without bespoke OF_* env wiring.
	if cfg.Database.URL == "" {
		cfg.Database.URL = os.Getenv("DATABASE_URL")
	}
	if cfg.KMS.Backend == "" {
		cfg.KMS.Backend = "local"
	}
	return &cfg, nil
}
