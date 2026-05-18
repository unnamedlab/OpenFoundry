// Package config loads the network-boundary-service configuration via
// koanf. The schema mirrors `docs/templates/service-skeleton` — only Service / Server
// / JWT / Telemetry knobs are exposed today; persistent backing stores are
// deferred until the ADR-0030 / S8.6 consolidation.
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

	Database struct {
		// URL is the writer-pool DSN. When empty the service boots
		// without persistence and falls back to the in-memory store —
		// useful in local dev where Postgres is unavailable.
		URL string `koanf:"url"`
		// ReadURL targets the CNPG read-replica. Optional.
		ReadURL string `koanf:"read_url"`
	} `koanf:"database"`
}

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
	if cfg.Database.URL == "" {
		cfg.Database.URL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if cfg.Database.ReadURL == "" {
		cfg.Database.ReadURL = strings.TrimSpace(os.Getenv("DATABASE_READ_URL"))
	}
	return &cfg, nil
}
