// Package config mirrors `services/sql-bi-gateway-service/src/config.rs`.
//
// All env vars use the `__` separator convention shared with
// sql-warehousing-service. Optional backend endpoints are deliberately
// nilable — when unset, statements that target the corresponding
// catalog are rejected with a clear error rather than silently routed
// elsewhere (see [`internal/routing`]).
package config

import (
	"os"
	"strconv"
)

// Config holds the runtime configuration of sql-bi-gateway-service.
type Config struct {
	Service struct {
		Name    string
		Version string
	}

	Host string
	// Port is the Flight SQL gRPC port (gateway primary surface).
	Port uint16
	// HealthzPort is the HTTP side router port (health + saved queries).
	HealthzPort uint16
	// PostgresWirePort is the Postgres wire (v3) listener BI tools
	// connect to when they prefer libpq over Arrow Flight SQL. Set to
	// 0 to disable the listener.
	PostgresWirePort uint16

	DatabaseURL string
	JWTSecret   string

	WarehousingFlightSQLURL string
	VespaFlightSQLURL       string
	PostgresFlightSQLURL    string
	TrinoFlightSQLURL       string

	// AllowAnonymous lets the Flight SQL surface accept unauthenticated
	// requests. Intended only for local dev / CI; production must keep
	// this false.
	AllowAnonymous bool
}

func FromEnv() (*Config, error) {
	c := &Config{}
	c.Service.Name = "sql-bi-gateway-service"
	c.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	c.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	c.Port = parseUint16(os.Getenv("PORT"), 50133)
	c.HealthzPort = parseUint16(os.Getenv("HEALTHZ_PORT"), 50134)
	c.PostgresWirePort = parseUint16(os.Getenv("POSTGRES_WIRE_PORT"), 5433)
	c.DatabaseURL = os.Getenv("DATABASE_URL")
	c.JWTSecret = os.Getenv("JWT_SECRET")
	c.WarehousingFlightSQLURL = os.Getenv("WAREHOUSING_FLIGHT_SQL_URL")
	c.VespaFlightSQLURL = os.Getenv("VESPA_FLIGHT_SQL_URL")
	c.PostgresFlightSQLURL = os.Getenv("POSTGRES_FLIGHT_SQL_URL")
	c.TrinoFlightSQLURL = os.Getenv("TRINO_FLIGHT_SQL_URL")
	c.AllowAnonymous = os.Getenv("ALLOW_ANONYMOUS") == "true"
	return c, nil
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
