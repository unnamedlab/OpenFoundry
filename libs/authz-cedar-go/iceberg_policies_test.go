package cedarauthz_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cedarauthz "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
)

// AllIcebergPolicies must validate against the bundled schema in
// strict mode — same invariant as the Rust
// `all_iceberg_policies_validate_against_schema` test.
func TestAllIcebergPoliciesValidateAgainstSchema(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	require.NoError(t, store.ReplacePolicies(cedarauthz.AllIcebergPolicies()))
	assert.GreaterOrEqual(t, store.Len(), 17, "expected at least 17 iceberg policies")
}

// Each policy id must be unique — duplicates would be rejected by
// PolicySet.Add anyway, but pin the inventory here so a refactor
// that accidentally collides two ids fails loudly.
func TestAllIcebergPoliciesHaveUniqueIDs(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, p := range cedarauthz.AllIcebergPolicies() {
		require.False(t, seen[p.ID], "duplicate policy id %q", p.ID)
		seen[p.ID] = true
	}
}

// Spot check a handful of policies in isolation.
func TestIcebergNamespaceViewClearanceParses(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	require.NoError(t, store.ReplacePolicies([]cedarauthz.PolicyRecord{
		cedarauthz.IcebergNamespaceViewClearance(),
	}))
	assert.Equal(t, 1, store.Len())
}

func TestIcebergTableManageMarkingsAdminOnlyParses(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	require.NoError(t, store.ReplacePolicies([]cedarauthz.PolicyRecord{
		cedarauthz.IcebergTableManageMarkingsAdminOnly(),
	}))
	assert.Equal(t, 1, store.Len())
}

// IDs locked: changing them invalidates audit-trail correlation.
func TestIcebergPolicyIDsLocked(t *testing.T) {
	t.Parallel()
	want := []string{
		"iceberg-namespace-view-clearance",
		"iceberg-namespace-create-user",
		"iceberg-namespace-create-service-principal",
		"iceberg-namespace-drop-admin",
		"iceberg-namespace-update-properties-user",
		"iceberg-namespace-update-properties-service-principal",
		"iceberg-namespace-manage-markings-admin",
		"iceberg-table-view-clearance",
		"iceberg-table-read-metadata-clearance",
		"iceberg-table-create-user",
		"iceberg-table-create-service-principal",
		"iceberg-table-drop-admin",
		"iceberg-table-write-data-user",
		"iceberg-table-write-data-service-principal",
		"iceberg-table-alter-schema-user",
		"iceberg-table-alter-schema-service-principal",
		"iceberg-table-manage-markings-admin",
	}
	got := make([]string, 0, len(cedarauthz.AllIcebergPolicies()))
	for _, p := range cedarauthz.AllIcebergPolicies() {
		got = append(got, p.ID)
	}
	assert.Equal(t, want, got, "policy id list and order locked — match Rust impl")
}
