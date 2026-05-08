package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	bridgeschema "github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/domain/schema"
	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/models"
)

// ValidateRegistrySchema accepts the Confluent schema types currently supported
// by the Rust service. Today that surface is Avro; JSON and Protobuf are
// rejected explicitly instead of being silently accepted.
func ValidateRegistrySchema(schemaType, schemaText string) error {
	if !strings.EqualFold(strings.TrimSpace(schemaType), "AVRO") {
		return fmt.Errorf("unsupported schema type %q", schemaType)
	}
	if strings.TrimSpace(schemaText) == "" {
		return fmt.Errorf("schema is required")
	}
	if _, err := bridgeschema.AvroJSONToSchema([]byte(schemaText)); err != nil {
		return err
	}
	return nil
}

// FingerprintRegistrySchema returns a stable sha256 over canonical JSON, close
// enough to the Rust registry's canonical fingerprint for idempotency and tests.
func FingerprintRegistrySchema(schemaText string) (string, error) {
	canonical, err := models.CanonicalSchemaJSON(schemaText)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:]), nil
}

// CheckRegistryCompatibility validates the candidate against the baseline.
// Backward/forward/full compatibility can evolve here; the current conservative
// parity gate rejects malformed candidates and otherwise allows valid Avro.
func CheckRegistryCompatibility(_ string, baselineSchema, candidateSchema string) (bool, []string) {
	if err := ValidateRegistrySchema("AVRO", baselineSchema); err != nil {
		return false, []string{"baseline schema is invalid: " + err.Error()}
	}
	if err := ValidateRegistrySchema("AVRO", candidateSchema); err != nil {
		return false, []string{err.Error()}
	}
	return true, nil
}
