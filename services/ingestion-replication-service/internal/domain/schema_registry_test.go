package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/domain"
)

func TestRegistrySchemaFingerprintCanonicalizesJSON(t *testing.T) {
	compact := `{"type":"record","name":"Order","fields":[{"name":"id","type":"string"}]}`
	spaced := `{ "name":"Order", "fields" : [ { "type":"string", "name":"id" } ], "type":"record" }`
	left, err := domain.FingerprintRegistrySchema(compact)
	require.NoError(t, err)
	right, err := domain.FingerprintRegistrySchema(spaced)
	require.NoError(t, err)
	require.Equal(t, left, right)
}

func TestValidateRegistrySchemaRejectsUnsupportedTypes(t *testing.T) {
	err := domain.ValidateRegistrySchema("JSON", `{}`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported schema type")
}
