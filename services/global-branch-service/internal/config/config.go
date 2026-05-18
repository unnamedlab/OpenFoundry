// Package config loads the global-branch-service configuration via
// koanf. See docs/templates/service-skeleton/internal/config for the precedence
// rules and shape rationale; this service mirrors that contract.
package config

import (
	"fmt"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config is the typed view of every knob the service exposes.
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

	// DatabaseURL is the Postgres DSN used by the repo layer. Empty
	// values are tolerated in dev/test (the binary skips the pool
	// init); production deployments inject the value via
	// OF_DATABASE_URL.
	DatabaseURL string `koanf:"database_url"`
}

// Load resolves configuration following the standard precedence:
//   - defaults baked into the image (defaultsPath),
//   - optional override file (envPath, typically CONFIG_FILE),
//   - OF_<KEY> environment variables.
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
	return &cfg, nil
}
