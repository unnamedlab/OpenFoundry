// Package config resolves action-log-sink configuration from the
// operator-facing environment contract. Mirrors the shape ai-sink and
// audit-sink expose so a single Helm values.yaml template can drive
// every sink.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	databus "github.com/openfoundry/openfoundry-go/libs/event-bus-data"
)

// Iceberg target — catalog + namespace + table shared by all action
// audit consumers.
const (
	IcebergCatalog   = "lakekeeper"
	IcebergNamespace = "default"
	IcebergTable     = "action_log"
)

// Topic + group constants. The consumer group name is pinned so
// rebalances + lag tracking land on a stable identifier across
// rolling deploys.
const (
	SourceTopic   = "ontology.actions.applied.v1"
	ConsumerGroup = "action-log-sink"
)

// Batch defaults mirror ai-sink: 100k records OR 60 seconds,
// whichever first.
const (
	BatchDefaultMaxRecords = 100_000
	BatchDefaultMaxWait    = 60 * time.Second
)

// Config is the resolved runtime configuration.
type Config struct {
	Service struct {
		Name    string
		Version string
	}
	DataBus        databus.Config
	CatalogURL     string
	TableWriterURL string
	Warehouse      string
	BatchPolicy    BatchPolicy
	MetricsAddr    string

	// JSONLWriterPath, when non-empty, selects the JSONL dev writer
	// instead of the Iceberg HTTP adapter.
	JSONLWriterPath string
}

// BatchPolicy mirrors ai-sink. ShouldFlush is read by the runtime
// every poll iteration.
type BatchPolicy struct {
	MaxRecords int
	MaxWait    time.Duration
}

// ShouldFlush returns true when either threshold is hit.
func (p BatchPolicy) ShouldFlush(records int, elapsed time.Duration) bool {
	return records >= p.MaxRecords || elapsed >= p.MaxWait
}

// FromEnv resolves Config or returns a typed *MissingEnvError /
// *InvalidEnvError so the operator runbook can pinpoint the bad knob.
func FromEnv() (*Config, error) {
	bootstrap := os.Getenv("KAFKA_BOOTSTRAP_SERVERS")
	if bootstrap == "" {
		return nil, &MissingEnvError{Key: "KAFKA_BOOTSTRAP_SERVERS"}
	}
	jsonlPath := os.Getenv("ACTION_LOG_SINK_JSONL_PATH")
	catalogURL := os.Getenv("ICEBERG_CATALOG_URL")
	if catalogURL == "" && jsonlPath == "" && os.Getenv("DATABASE_URL") == "" {
		return nil, &MissingEnvError{Key: "ICEBERG_CATALOG_URL"}
	}
	tableWriterURL := defaultStr(os.Getenv("ACTION_LOG_SINK_TABLE_WRITER_URL"), os.Getenv("ICEBERG_TABLE_WRITER_URL"))
	if tableWriterURL == "" {
		tableWriterURL = catalogURL
	}

	dbCfg := databus.NewConfig(splitCSV(bootstrap), buildPrincipal())

	policy := BatchPolicy{MaxRecords: BatchDefaultMaxRecords, MaxWait: BatchDefaultMaxWait}
	if v, ok, err := lookupInt("ACTION_LOG_SINK_BATCH_MAX_RECORDS"); err != nil {
		return nil, err
	} else if ok {
		policy.MaxRecords = v
	}
	if v, ok, err := lookupSecs("ACTION_LOG_SINK_BATCH_MAX_WAIT_SECONDS"); err != nil {
		return nil, err
	} else if ok {
		policy.MaxWait = v
	}

	cfg := &Config{
		DataBus:         dbCfg,
		CatalogURL:      catalogURL,
		TableWriterURL:  tableWriterURL,
		Warehouse:       os.Getenv("ICEBERG_WAREHOUSE"),
		BatchPolicy:     policy,
		MetricsAddr:     defaultStr(os.Getenv("METRICS_ADDR"), "0.0.0.0:9090"),
		JSONLWriterPath: jsonlPath,
	}
	cfg.Service.Name = "action-log-sink"
	cfg.Service.Version = defaultStr(os.Getenv("SERVICE_VERSION"), "dev")
	return cfg, nil
}

func buildPrincipal() databus.ServicePrincipal {
	password := os.Getenv("KAFKA_SASL_PASSWORD")
	username := defaultStr(os.Getenv("KAFKA_SASL_USERNAME"),
		defaultStr(os.Getenv("KAFKA_CLIENT_ID"), "action-log-sink"))
	if password == "" {
		return databus.InsecureDev(username)
	}
	mechanism := defaultStr(os.Getenv("KAFKA_SASL_MECHANISM"), databus.SASLMechanismScramSHA512)
	protocol := defaultStr(os.Getenv("KAFKA_SECURITY_PROTOCOL"), databus.SecurityProtocolSASLSSL)
	return databus.ServicePrincipal{
		Service:          username,
		Password:         password,
		Mechanism:        mechanism,
		SecurityProtocol: protocol,
	}
}

func splitCSV(v string) []string {
	out := []string{}
	current := ""
	for _, ch := range v {
		switch ch {
		case ',':
			if current != "" {
				out = append(out, current)
				current = ""
			}
		case ' ', '\t':
		default:
			current += string(ch)
		}
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}

func lookupInt(key string) (int, bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, false, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 0, false, &InvalidEnvError{Key: key, Value: raw, Reason: "expected positive integer"}
	}
	return v, true, nil
}

func lookupSecs(key string) (time.Duration, bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, false, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 0, false, &InvalidEnvError{Key: key, Value: raw, Reason: "expected positive integer (seconds)"}
	}
	return time.Duration(v) * time.Second, true, nil
}

func defaultStr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// MissingEnvError signals a required env var is unset.
type MissingEnvError struct{ Key string }

func (e *MissingEnvError) Error() string {
	return fmt.Sprintf("required environment variable %s is not set", e.Key)
}

// InvalidEnvError signals an env var is present but malformed.
type InvalidEnvError struct{ Key, Value, Reason string }

func (e *InvalidEnvError) Error() string {
	return fmt.Sprintf("invalid environment variable %s=%q: %s", e.Key, e.Value, e.Reason)
}

// IsMissingEnv reports whether err originates from FromEnv as missing-required.
func IsMissingEnv(err error) bool {
	var me *MissingEnvError
	return errors.As(err, &me)
}
