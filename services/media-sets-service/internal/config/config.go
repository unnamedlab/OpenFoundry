// Package config resolves media-sets-service env config.
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
	DatabaseURL              string
	JWTSecret                string
	MetricsAddr              string
	// MediaTransformRuntimeURL is the base URL of the
	// media-transform-runtime-service worker. When unset, defaults
	// to "http://media-transform-runtime-service:50173" (k8s in-
	// cluster DNS); the worker /transform endpoint is appended.
	MediaTransformRuntimeURL string
	// StorageBucket is the S3-compatible bucket the worker reads
	// bytes from. Falls back to "media" so dev clusters do not need
	// to declare it.
	StorageBucket string
	// StorageEndpoint is the public URL prefix the gateway proxies
	// presigned requests through. Bytes are signed against this
	// endpoint; the HMAC backend mints "<endpoint>/<bucket>/<key>".
	StorageEndpoint string
	// PresignTTLSeconds is the default URL lifetime when callers do
	// not supply expires_in_seconds. Defaults to 5 minutes.
	PresignTTLSeconds uint64
	// RetentionReaperIntervalSeconds is the cadence of the
	// retention sweep. Defaults to 60s.
	RetentionReaperIntervalSeconds uint64
	// ConnectorManagementServiceURL is the base URL of
	// connector-management-service. Empty disables the virtual
	// resolver, which falls back to the row's storage_uri verbatim.
	ConnectorManagementServiceURL string
	// GRPCPort is the TCP port the gRPC server listens on. Empty
	// (zero) disables the gRPC surface.
	GRPCPort uint16
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "media-sets-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50121)
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return nil, &MissingEnvError{Key: "DATABASE_URL"}
	}
	cfg.JWTSecret = defaultStr(os.Getenv("OPENFOUNDRY_JWT_SECRET"), os.Getenv("JWT_SECRET"))
	if cfg.JWTSecret == "" {
		return nil, &MissingEnvError{Key: "JWT_SECRET"}
	}
	cfg.MetricsAddr = defaultStr(os.Getenv("METRICS_ADDR"), "0.0.0.0:9090")
	cfg.MediaTransformRuntimeURL = defaultStr(
		os.Getenv("MEDIA_TRANSFORM_RUNTIME_URL"),
		"http://media-transform-runtime-service:50173",
	)
	cfg.StorageBucket = defaultStr(os.Getenv("MEDIA_STORAGE_BUCKET"), "media")
	cfg.StorageEndpoint = defaultStr(
		os.Getenv("MEDIA_STORAGE_ENDPOINT"),
		"http://edge-gateway-service",
	)
	cfg.PresignTTLSeconds = parseUint64(os.Getenv("MEDIA_PRESIGN_TTL_SECONDS"), 300)
	cfg.RetentionReaperIntervalSeconds = parseUint64(
		os.Getenv("MEDIA_RETENTION_REAPER_INTERVAL_SECONDS"), 60,
	)
	cfg.ConnectorManagementServiceURL = os.Getenv("CONNECTOR_MANAGEMENT_SERVICE_URL")
	cfg.GRPCPort = parseUint16(os.Getenv("MEDIA_SETS_GRPC_PORT"), 50122)
	return cfg, nil
}

func parseUint64(v string, fallback uint64) uint64 {
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
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
