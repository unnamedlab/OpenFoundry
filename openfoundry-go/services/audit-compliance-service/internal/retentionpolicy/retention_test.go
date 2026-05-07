package retentionpolicy

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

func mkPolicy(t *testing.T, system bool, sel models.RetentionSelector) models.RetentionPolicy {
	t.Helper()
	selJSON, _ := json.Marshal(sel)
	critJSON, _ := json.Marshal(models.RetentionCriteria{})
	return models.RetentionPolicy{
		ID:                  uuid.New(),
		Name:                "p",
		Scope:               "",
		TargetKind:          "transaction",
		RetentionDays:       0,
		LegalHold:           false,
		PurgeMode:           "hard-delete-after-ttl",
		Rules:               json.RawMessage(`[]`),
		UpdatedBy:           "system",
		Active:              true,
		IsSystem:            system,
		Selector:            selJSON,
		Criteria:            critJSON,
		GracePeriodMinutes:  60,
		CreatedAt:           time.Now().UTC(),
		UpdatedAt:           time.Now().UTC(),
	}
}

func TestEmptyQueryReturnsAll(t *testing.T) {
	t.Parallel()
	p := []models.RetentionPolicy{mkPolicy(t, false, models.RetentionSelector{})}
	got := FilterPolicies(p, &models.ListRetentionPoliciesQuery{})
	if len(got) != len(p) {
		t.Fatalf("got %d, want %d", len(got), len(p))
	}
}

func TestAllDatasetsSelectorMatchesAny(t *testing.T) {
	t.Parallel()
	p := []models.RetentionPolicy{mkPolicy(t, true, models.RetentionSelector{AllDatasets: true})}
	rid := "ri.x"
	q := &models.ListRetentionPoliciesQuery{DatasetRid: &rid}
	if got := FilterPolicies(p, q); len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
}

func TestExplicitDatasetRidFiltersOut(t *testing.T) {
	t.Parallel()
	matchRID := "ri.match"
	otherRID := "ri.other"
	policies := []models.RetentionPolicy{
		mkPolicy(t, false, models.RetentionSelector{DatasetRid: &matchRID}),
		mkPolicy(t, false, models.RetentionSelector{DatasetRid: &otherRID}),
	}
	q := &models.ListRetentionPoliciesQuery{DatasetRid: &matchRID}
	got := FilterPolicies(policies, q)
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	if sel, _ := models.SelectorFromRaw(got[0].Selector); sel.DatasetRid == nil || *sel.DatasetRid != matchRID {
		t.Fatalf("wrong policy returned")
	}
}

func TestSystemOnlyFiltersUserPolicies(t *testing.T) {
	t.Parallel()
	policies := []models.RetentionPolicy{
		mkPolicy(t, true, models.RetentionSelector{}),
		mkPolicy(t, false, models.RetentionSelector{}),
	}
	yes := true
	q := &models.ListRetentionPoliciesQuery{SystemOnly: &yes}
	got := FilterPolicies(policies, q)
	if len(got) != 1 || !got[0].IsSystem {
		t.Fatalf("expected one system policy, got %v", got)
	}
}

func TestResolveApplicableExplicitWinsOverInherited(t *testing.T) {
	t.Parallel()
	rid := "ri.foundry.dataset"
	projectID := uuid.New()
	explicitSelector := models.RetentionSelector{DatasetRid: &rid}
	projectSelector := models.RetentionSelector{ProjectID: &projectID}
	allSelector := models.RetentionSelector{AllDatasets: true}
	policies := []models.RetentionPolicy{
		mkPolicy(t, false, explicitSelector),
		mkPolicy(t, false, projectSelector),
		mkPolicy(t, true, allSelector),
	}
	resolved := ResolveApplicable(policies, rid, &models.ResolutionContext{ProjectID: &projectID})
	if len(resolved.Explicit) != 1 {
		t.Fatalf("expected 1 explicit, got %d", len(resolved.Explicit))
	}
	if len(resolved.Inherited.Project) != 1 {
		t.Fatalf("expected 1 inherited.project, got %d", len(resolved.Inherited.Project))
	}
	if len(resolved.Inherited.Org) != 1 {
		t.Fatalf("expected 1 inherited.org, got %d", len(resolved.Inherited.Org))
	}
	if resolved.Effective == nil {
		t.Fatal("effective must not be nil")
	}
	if resolved.Effective.ID != policies[0].ID {
		t.Fatalf("explicit policy must win, got %v", resolved.Effective.ID)
	}
}

func TestResolveApplicableLegalHoldWins(t *testing.T) {
	t.Parallel()
	rid := "ri.x"
	hold := mkPolicy(t, false, models.RetentionSelector{DatasetRid: &rid})
	hold.LegalHold = true
	hold.RetentionDays = 365
	short := mkPolicy(t, false, models.RetentionSelector{DatasetRid: &rid})
	short.RetentionDays = 7
	resolved := ResolveApplicable([]models.RetentionPolicy{short, hold}, rid, &models.ResolutionContext{})
	if resolved.Effective == nil || !resolved.Effective.LegalHold {
		t.Fatal("legal_hold must win the resolution")
	}
}
