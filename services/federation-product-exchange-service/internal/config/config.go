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
	// Products feature — see internal/products. The sign key is the
	// symmetric HMAC-SHA256 secret used to authenticate bundles; the
	// bundle root is the on-disk root used by the filesystem storage
	// (empty → in-memory storage, which is suitable for tests but not
	// for prod). The four service URLs are the base URLs of the
	// owner services called by Publish/Install.
	MarketplaceSignKey        string
	MarketplaceBundleRoot     string
	OntologyDefinitionURL     string
	OntologyActionsURL        string
	PipelineBuildURL          string
	ApplicationCompositionURL string
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "federation-product-exchange-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50120)
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	cfg.MarketplaceDatabaseURL = os.Getenv("MARKETPLACE_DATABASE_URL")
	cfg.JWTSecret = os.Getenv("JWT_SECRET")
	cfg.MarketplaceSignKey = os.Getenv("MARKETPLACE_SIGN_KEY")
	cfg.MarketplaceBundleRoot = os.Getenv("MARKETPLACE_BUNDLE_ROOT")
	cfg.OntologyDefinitionURL = os.Getenv("ONTOLOGY_DEFINITION_URL")
	cfg.OntologyActionsURL = os.Getenv("ONTOLOGY_ACTIONS_URL")
	cfg.PipelineBuildURL = os.Getenv("PIPELINE_BUILD_URL")
	cfg.ApplicationCompositionURL = os.Getenv("APPLICATION_COMPOSITION_URL")
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
