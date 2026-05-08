package authz

import (
	"context"
	"errors"
	"testing"
)

func newPrincipal(tenant string, scopes ...string) *Principal {
	set := make(map[string]struct{}, len(scopes))
	for _, s := range scopes {
		set[s] = struct{}{}
	}
	return &Principal{
		Subject: "00000000-0000-0000-0000-000000000001",
		Scopes:  set,
		Kind:    PrincipalKindFromScopes(set),
		Tenant:  tenant,
	}
}

func TestAdminRoleExpandsClearanceLadder(t *testing.T) {
	p := newPrincipal("default", "role:admin")
	clearances := principalClearances(p)
	for _, want := range []string{"public", "confidential", "pii", "restricted", "secret"} {
		if !contains(clearances, want) {
			t.Fatalf("missing %q in %v", want, clearances)
		}
	}
}

func TestDenialReasonPrefersScopeOverClearance(t *testing.T) {
	engine := NewPolicyEngine("default")
	p := newPrincipal("default", "api:iceberg-read", "iceberg-clearance:public")
	resource := TableResource(TableAttrs{
		RID:           "t",
		NamespaceRID:  "n",
		Tenant:        "default",
		FormatVersion: 2,
		Markings:      []string{"pii"},
	})
	err := engine.Enforce(context.Background(), p, "iceberg::table::write_data", resource)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("expected *DenyError, got %v", err)
	}
	if deny.Reason != ReasonMissingScope {
		t.Fatalf("expected MissingScope, got %s", deny.Reason)
	}
}

func TestMissingClearanceDetectedWhenScopePresent(t *testing.T) {
	engine := NewPolicyEngine("default")
	p := newPrincipal("default", "api:iceberg-write", "iceberg-clearance:public")
	resource := TableResource(TableAttrs{
		RID:           "t",
		NamespaceRID:  "n",
		Tenant:        "default",
		FormatVersion: 2,
		Markings:      []string{"pii"},
	})
	err := engine.Enforce(context.Background(), p, "iceberg::table::write_data", resource)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("expected *DenyError, got %v", err)
	}
	if deny.Reason != ReasonMissingClearance {
		t.Fatalf("expected MissingClearance, got %s", deny.Reason)
	}
}

func TestEngineAllowsCleanRead(t *testing.T) {
	engine := NewPolicyEngine("default")
	p := newPrincipal("default", "api:iceberg-read", "iceberg-clearance:public")
	resource := TableResource(TableAttrs{
		RID:      "t",
		Tenant:   "default",
		Markings: []string{"public"},
	})
	if err := engine.Enforce(context.Background(), p, "iceberg::table::view", resource); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestEngineDeniesMutationWithoutWriteScope(t *testing.T) {
	engine := NewPolicyEngine("default")
	p := newPrincipal("default", "api:iceberg-read", "role:admin")
	resource := TableResource(TableAttrs{RID: "t", Tenant: "default"})
	err := engine.Enforce(context.Background(), p, "iceberg::table::create", resource)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("expected *DenyError, got %v", err)
	}
	if deny.Reason != ReasonMissingScope {
		t.Fatalf("expected MissingScope, got %s", deny.Reason)
	}
}

func TestEngineDeniesManageMarkingsWithoutRole(t *testing.T) {
	engine := NewPolicyEngine("default")
	p := newPrincipal("default", "api:iceberg-write", "iceberg-clearance:public")
	resource := TableResource(TableAttrs{
		RID:      "t",
		Tenant:   "default",
		Markings: []string{"public"},
	})
	err := engine.Enforce(context.Background(), p, "iceberg::table::manage_markings", resource)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("expected *DenyError, got %v", err)
	}
	if deny.Reason != ReasonMissingRole {
		t.Fatalf("expected MissingRole, got %s", deny.Reason)
	}
}

func TestEngineAllowsManageMarkingsForAdmin(t *testing.T) {
	engine := NewPolicyEngine("default")
	p := newPrincipal("default", "api:iceberg-write", "role:admin")
	resource := TableResource(TableAttrs{
		RID:      "t",
		Tenant:   "default",
		Markings: []string{"pii"},
	})
	if err := engine.Enforce(context.Background(), p, "iceberg::table::manage_markings", resource); err != nil {
		t.Fatalf("expected allow, got %v", err)
	}
}

func TestEngineDeniesAcrossTenants(t *testing.T) {
	engine := NewPolicyEngine("default")
	p := newPrincipal("alpha", "api:iceberg-read", "role:admin")
	resource := TableResource(TableAttrs{RID: "t", Tenant: "beta"})
	err := engine.Enforce(context.Background(), p, "iceberg::table::view", resource)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("expected *DenyError, got %v", err)
	}
	if deny.Reason != ReasonOutOfTenant {
		t.Fatalf("expected OutOfTenant, got %s", deny.Reason)
	}
}

func TestPrincipalKindFromScopes(t *testing.T) {
	user := PrincipalKindFromScopes(map[string]struct{}{"api:iceberg-read": {}})
	if user != PrincipalUser {
		t.Fatalf("expected User, got %s", user)
	}
	svc := PrincipalKindFromScopes(map[string]struct{}{"svc:builder": {}, "api:iceberg-write": {}})
	if svc != PrincipalServicePrincipal {
		t.Fatalf("expected ServicePrincipal, got %s", svc)
	}
}
