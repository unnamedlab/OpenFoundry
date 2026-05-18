// Package config loads the global-branch-service configuration via
// koanf. See docs/templates/service-skeleton/internal/config for the precedence
// rules and shape rationale; this service mirrors that contract.
package config

import (
	"errors"
	"fmt"
	"os"
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

	// Environment identifies the runtime environment. Values "prod"
	// and "production" enable fail-closed boot checks.
	Environment string `koanf:"environment"`

	// AllowUnwiredProductRoutes is an explicit dev/test smoke-mode
	// escape hatch. When true outside production, the process may boot
	// without a DB and product routes remain unmounted.
	AllowUnwiredProductRoutes bool `koanf:"allow_unwired_product_routes"`

	// DatabaseURL is the Postgres DSN used by the repo layer. Product
	// routes require a real database; empty values are accepted only
	// for explicit non-production smoke mode. Production deployments
	// inject this via OF_DATABASE_URL or DATABASE_URL.
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
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		cfg.DatabaseURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	return &cfg, nil
}

// ErrDatabaseRequired is returned when product routes cannot be safely
// wired because no real database DSN was configured.
var ErrDatabaseRequired = errors.New("global-branch-service: database_url required for product routes")

// IsProduction reports whether the runtime environment should use
// fail-closed production boot policy.
func (c *Config) IsProduction() bool {
	switch strings.ToLower(strings.TrimSpace(c.Environment)) {
	case "prod", "production":
		return true
	default:
		return false
	}
}

// ProductRoutesUnwiredAllowed reports whether this process may boot
// with handlers intentionally unwired for smoke/dev checks. It is
// always false in production.
func (c *Config) ProductRoutesUnwiredAllowed() bool {
	return !c.IsProduction() && c.AllowUnwiredProductRoutes
}

// ValidateProductDatabase enforces fail-closed startup for the global
// branch product surface. A real DB is mandatory unless explicit
// non-production smoke mode is enabled.
func (c *Config) ValidateProductDatabase() error {
	if strings.TrimSpace(c.DatabaseURL) != "" {
		return nil
	}
	if c.ProductRoutesUnwiredAllowed() {
		return nil
	}
	if c.IsProduction() {
		return fmt.Errorf("%w: production environment %q has no DATABASE_URL/OF_DATABASE_URL", ErrDatabaseRequired, c.Environment)
	}
	return fmt.Errorf("%w: set DATABASE_URL/OF_DATABASE_URL or enable allow_unwired_product_routes for explicit smoke/dev mode", ErrDatabaseRequired)
}
