// Package config mirrors `services/notebook-runtime-service/src/config.rs`.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	Service struct {
		Name    string
		Version string
	}

	Host string
	Port uint16

	DatabaseURL string
	JWTSecret   string

	DataDir         string
	QueryServiceURL string
	AIServiceURL    string
}

func FromEnv() (*Config, error) {
	c := &Config{}
	c.Service.Name = "notebook-runtime-service"
	c.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	c.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	c.Port = parseUint16(os.Getenv("PORT"), 50134)
	c.DatabaseURL = os.Getenv("DATABASE_URL")
	c.JWTSecret = os.Getenv("JWT_SECRET")
	c.DataDir = defaultStr(os.Getenv("DATA_DIR"), "/tmp/notebook-data")
	c.QueryServiceURL = defaultStr(os.Getenv("QUERY_SERVICE_URL"), "http://127.0.0.1:50133")
	c.AIServiceURL = defaultStr(os.Getenv("AI_SERVICE_URL"), "http://127.0.0.1:50127")
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
