package retention_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/domain/retention"
	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/models"
)

func ts(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func uid(n uint64) uuid.UUID {
	var b [16]byte
	for i := 7; i >= 0; i-- {
		b[15-i] = byte(n >> (i * 8))
	}
	return uuid.UUID(b)
}

func row(id uint64, parent *uint64, policy models.RetentionPolicy, ttl *int32) models.RetentionRow {
	r := models.RetentionRow{
		ID:                 uid(id),
		Policy:             policy,
		TTLDays:            ttl,
		LastActivityAt:     ts(2026, time.January, 1),
		HasOpenTransaction: false,
		IsRoot:             parent == nil,
	}
	if parent != nil {
		p := uid(*parent)
		r.ParentBranchID = &p
	}
	return r
}

// Mirrors Rust `explicit_forever_short_circuits_resolution`.
func TestExplicitForeverShortCircuitsResolution(t *testing.T) {
	r := row(1, nil, models.RetentionPolicyForever, nil)
	idx := retention.IndexRows([]models.RetentionRow{r})
	eff := retention.ResolveEffective(r, idx)
	require.Equal(t, models.RetentionPolicyForever, eff.Policy)
	require.NotNil(t, eff.SourceBranchID)
	require.Equal(t, r.ID, *eff.SourceBranchID)
}

// Mirrors Rust `inherited_walks_to_first_explicit_ancestor`.
func TestInheritedWalksToFirstExplicitAncestor(t *testing.T) {
	master := row(1, nil, models.RetentionPolicyForever, nil)
	one := uint64(1)
	two := uint64(2)
	develop := row(2, &one, models.RetentionPolicyInherited, nil)
	feature := row(3, &two, models.RetentionPolicyInherited, nil)
	idx := retention.IndexRows([]models.RetentionRow{master, develop, feature})
	eff := retention.ResolveEffective(feature, idx)
	require.Equal(t, models.RetentionPolicyForever, eff.Policy)
	require.NotNil(t, eff.SourceBranchID)
	require.Equal(t, master.ID, *eff.SourceBranchID)
}

// Mirrors Rust `inherited_chain_without_explicit_defaults_to_forever`.
func TestInheritedChainWithoutExplicitDefaultsToForever(t *testing.T) {
	parent := row(1, nil, models.RetentionPolicyInherited, nil)
	one := uint64(1)
	child := row(2, &one, models.RetentionPolicyInherited, nil)
	idx := retention.IndexRows([]models.RetentionRow{parent, child})
	eff := retention.ResolveEffective(child, idx)
	require.Equal(t, models.RetentionPolicyForever, eff.Policy)
	require.Nil(t, eff.SourceBranchID)
}

// Mirrors Rust `ttl_eligibility_respects_open_transaction_invariant`.
func TestTTLEligibilityRespectsOpenTransactionInvariant(t *testing.T) {
	now := ts(2026, time.June, 1)
	ttl := int32(30)
	feature := row(1, nil, models.RetentionPolicyTTLDays, &ttl)
	feature.IsRoot = false
	pid := uid(99)
	feature.ParentBranchID = &pid
	feature.LastActivityAt = ts(2026, time.January, 1)
	feature.HasOpenTransaction = true
	srcID := feature.ID
	eff := models.EffectiveRetention{
		Policy:         models.RetentionPolicyTTLDays,
		TTLDays:        &ttl,
		SourceBranchID: &srcID,
	}
	require.False(t, retention.IsArchiveEligible(feature, eff, now))
}

// Mirrors Rust `ttl_eligibility_archives_stale_non_root_branch`.
func TestTTLEligibilityArchivesStaleNonRootBranch(t *testing.T) {
	now := ts(2026, time.June, 1)
	ttl := int32(30)
	feature := row(1, nil, models.RetentionPolicyTTLDays, &ttl)
	feature.IsRoot = false
	pid := uid(99)
	feature.ParentBranchID = &pid
	feature.LastActivityAt = ts(2026, time.April, 1) // 60 days ago
	srcID := feature.ID
	eff := models.EffectiveRetention{
		Policy:         models.RetentionPolicyTTLDays,
		TTLDays:        &ttl,
		SourceBranchID: &srcID,
	}
	require.True(t, retention.IsArchiveEligible(feature, eff, now))
}

// Mirrors Rust `forever_branches_are_never_eligible`.
func TestForeverBranchesAreNeverEligible(t *testing.T) {
	now := ts(2026, time.June, 1)
	master := row(1, nil, models.RetentionPolicyForever, nil)
	master.LastActivityAt = ts(2025, time.January, 1)
	srcID := master.ID
	eff := models.EffectiveRetention{
		Policy:         models.RetentionPolicyForever,
		TTLDays:        nil,
		SourceBranchID: &srcID,
	}
	require.False(t, retention.IsArchiveEligible(master, eff, now))
}

// Mirrors Rust `root_branches_are_never_eligible_even_with_ttl`.
func TestRootBranchesAreNeverEligibleEvenWithTTL(t *testing.T) {
	now := ts(2026, time.June, 1)
	ttl := int32(1)
	root := row(1, nil, models.RetentionPolicyTTLDays, &ttl)
	root.IsRoot = true
	root.LastActivityAt = ts(2026, time.April, 1)
	srcID := root.ID
	eff := models.EffectiveRetention{
		Policy:         models.RetentionPolicyTTLDays,
		TTLDays:        &ttl,
		SourceBranchID: &srcID,
	}
	require.False(t, retention.IsArchiveEligible(root, eff, now))
}

// Cycle in parent chain falls back to FOREVER (Rust defensive guard).
func TestCycleInParentChainFallsBackToForever(t *testing.T) {
	one := uint64(1)
	two := uint64(2)
	a := row(1, &two, models.RetentionPolicyInherited, nil)
	b := row(2, &one, models.RetentionPolicyInherited, nil)
	idx := retention.IndexRows([]models.RetentionRow{a, b})
	eff := retention.ResolveEffective(a, idx)
	require.Equal(t, models.RetentionPolicyForever, eff.Policy)
	require.Nil(t, eff.SourceBranchID)
}

// TTLDays <= 0 is never eligible (Rust guard).
func TestTTLDaysNonPositiveNeverEligible(t *testing.T) {
	now := ts(2026, time.June, 1)
	ttl := int32(0)
	feature := row(1, nil, models.RetentionPolicyTTLDays, &ttl)
	feature.IsRoot = false
	pid := uid(99)
	feature.ParentBranchID = &pid
	feature.LastActivityAt = ts(2024, time.January, 1)
	srcID := feature.ID
	eff := models.EffectiveRetention{Policy: models.RetentionPolicyTTLDays, TTLDays: &ttl, SourceBranchID: &srcID}
	require.False(t, retention.IsArchiveEligible(feature, eff, now))
}

// Already-archived rows are skipped even if otherwise eligible.
func TestAlreadyArchivedRowsAreSkipped(t *testing.T) {
	now := ts(2026, time.June, 1)
	ttl := int32(1)
	feature := row(1, nil, models.RetentionPolicyTTLDays, &ttl)
	feature.IsRoot = false
	pid := uid(99)
	feature.ParentBranchID = &pid
	feature.LastActivityAt = ts(2025, time.January, 1)
	archived := ts(2026, time.May, 1)
	feature.ArchivedAt = &archived
	srcID := feature.ID
	eff := models.EffectiveRetention{Policy: models.RetentionPolicyTTLDays, TTLDays: &ttl, SourceBranchID: &srcID}
	require.False(t, retention.IsArchiveEligible(feature, eff, now))
}

func TestParsePolicyRoundTrip(t *testing.T) {
	for _, label := range []string{"INHERITED", "FOREVER", "TTL_DAYS"} {
		p, ok := retention.ParsePolicy(label)
		require.True(t, ok)
		require.Equal(t, label, retention.PolicyAsString(p))
	}
	_, ok := retention.ParsePolicy("BOGUS")
	require.False(t, ok)
}

func TestCountEligibleAggregates(t *testing.T) {
	now := ts(2026, time.June, 1)
	ttl := int32(30)
	pid := uid(99)
	stale := row(1, nil, models.RetentionPolicyTTLDays, &ttl)
	stale.IsRoot = false
	stale.ParentBranchID = &pid
	stale.LastActivityAt = ts(2026, time.January, 1)

	fresh := row(2, nil, models.RetentionPolicyTTLDays, &ttl)
	fresh.IsRoot = false
	fresh.ParentBranchID = &pid
	fresh.LastActivityAt = ts(2026, time.May, 31)

	master := row(3, nil, models.RetentionPolicyForever, nil)
	rows := []models.RetentionRow{stale, fresh, master}
	require.Equal(t, 1, retention.CountEligible(rows, retention.IndexRows(rows), now))
}
