// Package config resolves code-repository-review-service env config.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Service struct{ Name, Version string }
	Server  struct {
		Host string
		Port uint16
	}
	DatabaseURL               string
	JWTSecret                 string
	Actor                     string
	KafkaBrokers              []string
	BranchEventsConsumerGroup string
	GitStorageRoot            string
	GitHTTPBaseURL            string
	GitSSHBaseURL             string
	GitSSHEnabled             bool
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "code-repository-review-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50155)
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	cfg.JWTSecret = os.Getenv("JWT_SECRET")
	cfg.Actor = defaultStr(os.Getenv("SERVICE_ACTOR"), "system")
	cfg.KafkaBrokers = splitCSV(os.Getenv("KAFKA_BROKERS"))
	cfg.BranchEventsConsumerGroup = defaultStr(os.Getenv("BRANCH_EVENTS_CONSUMER_GROUP"), "code-repository-review-service.branch-events")
	cfg.GitStorageRoot = defaultStr(os.Getenv("CODE_REPOSITORY_GIT_ROOT"), "/var/lib/openfoundry/code-repositories")
	cfg.GitHTTPBaseURL = os.Getenv("CODE_REPOSITORY_GIT_HTTP_BASE_URL")
	cfg.GitSSHBaseURL = os.Getenv("CODE_REPOSITORY_GIT_SSH_BASE_URL")
	cfg.GitSSHEnabled = parseBool(os.Getenv("CODE_REPOSITORY_GIT_SSH_ENABLED"))

	if cfg.DatabaseURL == "" {
		return nil, &MissingEnvError{Key: "DATABASE_URL"}
	}
	if cfg.JWTSecret == "" {
		return nil, &MissingEnvError{Key: "JWT_SECRET"}
	}
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

func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}
