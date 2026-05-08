package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/domain"
)

func TestMarkingRank(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 0, domain.MarkingRank("public"))
	assert.Equal(t, 1, domain.MarkingRank("confidential"))
	assert.Equal(t, 2, domain.MarkingRank("pii"))
	assert.Equal(t, -1, domain.MarkingRank("banana"))
}

func TestValidateMarking(t *testing.T) {
	t.Parallel()
	require.NoError(t, domain.ValidateMarking("public"))
	require.NoError(t, domain.ValidateMarking("confidential"))
	require.NoError(t, domain.ValidateMarking("pii"))
	require.Error(t, domain.ValidateMarking("secret"))
}

func TestNormalizeMarkings(t *testing.T) {
	t.Parallel()
	got, err := domain.NormalizeMarkings([]string{" PII ", "public", "PII", ""})
	require.NoError(t, err)
	assert.Equal(t, []string{"pii", "public"}, got, "lower + sort + dedup")
}

func TestNormalizeMarkingsRejectsInvalid(t *testing.T) {
	t.Parallel()
	_, err := domain.NormalizeMarkings([]string{"public", "bogus"})
	require.Error(t, err)
}

func TestMaxMarking(t *testing.T) {
	t.Parallel()
	v, ok := domain.MaxMarking([]string{"public", "pii", "confidential"})
	require.True(t, ok)
	assert.Equal(t, "pii", v)

	_, ok = domain.MaxMarking([]string{"unknown"})
	assert.False(t, ok)
}

func TestMarkingsForClearance(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"public"}, domain.MarkingsForClearance("public"))
	assert.Equal(t, []string{"public", "confidential"}, domain.MarkingsForClearance("confidential"))
	assert.Equal(t, []string{"public", "confidential", "pii"}, domain.MarkingsForClearance("pii"))
	// Unknown / empty → public only.
	assert.Equal(t, []string{"public"}, domain.MarkingsForClearance(""))
	assert.Equal(t, []string{"public"}, domain.MarkingsForClearance("banana"))
}
