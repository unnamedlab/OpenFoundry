package cedarauthz_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cedarauthz "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
)

// AllSchedulePolicies must validate against the bundled schema in
// strict mode — equivalent to the Rust
// `all_policies_parse_against_the_schema` test.
func TestAllSchedulePoliciesValidateAgainstSchema(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	require.NoError(t, store.ReplacePolicies(cedarauthz.AllSchedulePolicies()))
	assert.Equal(t, 5, store.Len())
}

func TestAllSchedulePoliciesHaveUniqueIDs(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, p := range cedarauthz.AllSchedulePolicies() {
		require.False(t, seen[p.ID], "duplicate policy id %q", p.ID)
		seen[p.ID] = true
	}
}

func TestSchedulePolicyIDsLocked(t *testing.T) {
	t.Parallel()
	want := []string{
		"schedule-edit-owner-or-editor",
		"schedule-pause-resume-editor",
		"schedule-convert-requires-manage",
		"build-run-service-principal-in-scope",
		"build-run-user-clearance",
	}
	got := make([]string, 0, len(cedarauthz.AllSchedulePolicies()))
	for _, p := range cedarauthz.AllSchedulePolicies() {
		got = append(got, p.ID)
	}
	assert.Equal(t, want, got, "policy id list and order locked — match Rust impl")
}

// build-run-service-principal-in-scope references
// `principal.project_scope_rids` — pin it parses against the schema in
// isolation so a future schema refactor doesn't silently drop the
// attribute.
func TestBuildRunServicePrincipalInScopeParses(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	require.NoError(t, store.ReplacePolicies([]cedarauthz.PolicyRecord{
		cedarauthz.BuildRunServicePrincipalInScope(),
	}))
	assert.Equal(t, 1, store.Len())
}

// schedule-convert-requires-manage hinges on the virtual role
// `manage_all_target_projects` — locked here so the doc invariant
// stays intact (the handler injects this role; Cedar can't iterate
// project lists).
func TestScheduleConvertRequiresManageReferencesVirtualRole(t *testing.T) {
	t.Parallel()
	policy := cedarauthz.ScheduleConvertRequiresManage()
	assert.Contains(t, policy.Source, `manage_all_target_projects`,
		"schedule::convert_to_project_scope must keep the virtual-role contract")
}

// Cross-suite invariant: iceberg + schedule policies coexist without
// id collisions and validate together.
func TestIcebergAndSchedulePoliciesCoexist(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	all := append(cedarauthz.AllIcebergPolicies(), cedarauthz.AllSchedulePolicies()...)
	require.NoError(t, store.ReplacePolicies(all))
	assert.Equal(t, len(all), store.Len())
}
