// Package config resolves workflow-automation-service env config.
//
// Post-S8 ownership boundary: this binary owns workflow definitions/runs
// (legacy workflow-automation-service) + saga substrate (legacy
// automation-operations-service) + human-in-the-loop approvals state
// machine (legacy approvals-service). See ADR-0030.
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
	DatabaseURL                  string
	JWTSecret                    string
	NotificationServiceURL       string
	OntologyServiceURL           string
	PipelineServiceURL           string
	NATSURL                      string
	AuditComplianceServiceURL    string
	AuditComplianceBearerToken   string
	ApprovalTTLHours             uint32
	KafkaBootstrap               string
}

func FromEnv() (*Config, error) {
	cfg := &Config{}
	cfg.Service.Name = "workflow-automation-service"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	cfg.Server.Host = defaultStr(os.Getenv("HOST"), "0.0.0.0")
	cfg.Server.Port = parseUint16(os.Getenv("PORT"), 50137)
	cfg.DatabaseURL = os.Getenv("DATABASE_URL")
	cfg.JWTSecret = os.Getenv("JWT_SECRET")
	cfg.NotificationServiceURL = defaultStr(os.Getenv("NOTIFICATION_SERVICE_URL"), "http://localhost:50114")
	cfg.OntologyServiceURL = defaultStr(os.Getenv("ONTOLOGY_SERVICE_URL"), "http://localhost:50106")
	cfg.PipelineServiceURL = defaultStr(os.Getenv("PIPELINE_SERVICE_URL"), "http://localhost:50083")
	cfg.NATSURL = defaultStr(os.Getenv("NATS_URL"), "nats://localhost:4222")
	cfg.AuditComplianceServiceURL = defaultStr(os.Getenv("AUDIT_COMPLIANCE_SERVICE_URL"), "http://localhost:50115")
	cfg.AuditComplianceBearerToken = os.Getenv("AUDIT_COMPLIANCE_BEARER_TOKEN")
	cfg.ApprovalTTLHours = parseUint32(os.Getenv("APPROVAL_TTL_HOURS"), 24)
	cfg.KafkaBootstrap = os.Getenv("KAFKA_BOOTSTRAP_SERVERS")
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

func parseUint32(v string, fallback uint32) uint32 {
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return fallback
	}
	return uint32(n)
}
