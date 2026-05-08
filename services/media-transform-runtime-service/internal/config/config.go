// Package config resolves media-transform-runtime-service env config.
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
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "media-transform-runtime-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	// MEDIA_TRANSFORM_HOST is the *consumer-side* hostname callers use
	// to reach this service; using it as the bind host fails in-cluster
	// because it resolves to the Service ClusterIP, which the pod
	// cannot bind. Bind to HOST (default 0.0.0.0).
	cfg.Server.Host = firstStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(firstStr(os.Getenv("MEDIA_TRANSFORM_PORT"), os.Getenv("PORT")), 50173)
	return cfg, nil
}

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func firstStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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
