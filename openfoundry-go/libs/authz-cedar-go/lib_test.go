package cedarauthz_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cedarauthz "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
)

// Bundled schema must parse — same invariant as the Rust
// `empty_store_loads_bundled_schema` test.
func TestNewEmptyParsesBundledSchema(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	require.NotNil(t, store)
	assert.True(t, store.IsEmpty())
	assert.Equal(t, 0, store.Len())
	assert.NotNil(t, store.Schema())
	assert.NotNil(t, store.ResolvedSchema())
}

// ReplacePolicies validates against the bundled schema in strict mode —
// equivalent to the Rust `replace_policies_validates_against_schema` test.
func TestReplacePoliciesAcceptsValidClearancePolicy(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	policy := cedarauthz.PolicyRecord{
		ID:      "default-allow-clearance",
		Version: 1,
		Source: `
			permit(
			  principal,
			  action == Action::"read",
			  resource is Dataset
			) when {
			  resource.markings.containsAll(principal.clearances) ||
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
	require.NoError(t, store.ReplacePolicies([]cedarauthz.PolicyRecord{policy}))
	assert.Equal(t, 1, store.Len())
	assert.False(t, store.IsEmpty())
}

// Garbage policy text is rejected with PolicyParseError carrying the id.
func TestReplacePoliciesRejectsInvalidPolicyText(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	bad := cedarauthz.PolicyRecord{
		ID:     "broken",
		Source: "this is not cedar",
	}
	err = store.ReplacePolicies([]cedarauthz.PolicyRecord{bad})
	require.Error(t, err)
	var ppe *cedarauthz.PolicyParseError
	require.True(t, errors.As(err, &ppe), "want PolicyParseError, got %T", err)
	assert.Equal(t, "broken", ppe.ID)
	// Store remains empty — atomic swap semantics.
	assert.Equal(t, 0, store.Len())
}

// Schema-incompatible policy (refers to undeclared attribute) must
// fail strict validation. Mirrors the Rust ValidationMode::Strict gate.
func TestReplacePoliciesRejectsSchemaIncompatible(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	// `principal.clearances` exists; `resource.bogus_attr` does NOT in
	// the bundled schema — strict mode must reject.
	policy := cedarauthz.PolicyRecord{
		ID:     "schema-violation",
		Source: `permit(principal, action == Action::"read", resource is Dataset) when { resource.bogus_attr == "x" };`,
	}
	err = store.ReplacePolicies([]cedarauthz.PolicyRecord{policy})
	require.Error(t, err)
	var ve *cedarauthz.ValidationError
	require.True(t, errors.As(err, &ve), "want ValidationError, got %T", err)
	require.True(t, errors.Is(err, cedarauthz.ErrValidation))
	assert.Equal(t, 0, store.Len(), "swap is atomic — bad set is discarded")
}

// Duplicate policy ids within the same input are rejected.
func TestReplacePoliciesRejectsDuplicateIDs(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	src := `permit(principal, action == Action::"read", resource is Dataset);`
	err = store.ReplacePolicies([]cedarauthz.PolicyRecord{
		{ID: "dup", Source: src},
		{ID: "dup", Source: src},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, cedarauthz.ErrValidation))
}

// Multiple valid policies all land atomically.
func TestReplacePoliciesAtomicMultipleValid(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewWithPolicies([]cedarauthz.PolicyRecord{
		{ID: "p1", Source: `permit(principal, action == Action::"read", resource is Dataset);`},
		{ID: "p2", Source: `permit(principal, action == Action::"write", resource is Dataset);`},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, store.Len())
}
