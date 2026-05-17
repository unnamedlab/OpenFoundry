package security

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

func claims(clearance string) *authmw.Claims {
	attrs, _ := json.Marshal(map[string]string{"classification_clearance": clearance})
	return &authmw.Claims{
		Sub:         uuid.Nil,
		Roles:       []string{"viewer"},
		Permissions: []string{"audit-logs:view"},
		Attributes:  attrs,
	}
}

func adminClaims() *authmw.Claims {
	return &authmw.Claims{Sub: uuid.Nil, Roles: []string{"admin"}}
}

func event(level models.ClassificationLevel) *models.AuditEvent {
	subject := "subject-1"
	meta, _ := json.Marshal(map[string]string{"organization_id": uuid.Nil.String()})
	return &models.AuditEvent{
		ID:             uuid.New(),
		Classification: string(level),
		SubjectID:      &subject,
		Metadata:       meta,
	}
}

func TestClearanceConfidentialAllowsConfidentialNotPii(t *testing.T) {
	t.Parallel()
	c := claims("confidential")
	if !CanAccessEvent(event(models.ClassificationPublic), c) {
		t.Fatal("confidential clearance should see public")
	}
	if !CanAccessEvent(event(models.ClassificationConfidential), c) {
		t.Fatal("confidential clearance should see confidential")
	}
	if CanAccessEvent(event(models.ClassificationPii), c) {
		t.Fatal("confidential clearance must NOT see pii")
	}
}

func TestAdminPassesEverything(t *testing.T) {
	t.Parallel()
	a := adminClaims()
	for _, level := range []models.ClassificationLevel{
		models.ClassificationPublic,
		models.ClassificationConfidential,
		models.ClassificationPii,
	} {
		if !CanAccessEvent(event(level), a) {
			t.Fatalf("admin must pass %s", level)
		}
	}
}

func TestNoClaimsRejected(t *testing.T) {
	t.Parallel()
	if CanAccessEvent(event(models.ClassificationPublic), nil) {
		t.Fatal("nil claims must be rejected")
	}
}

func TestAuditLogAccessRequiresDedicatedPermission(t *testing.T) {
	t.Parallel()
	c := &authmw.Claims{Sub: uuid.Nil, Roles: []string{"viewer"}}
	if CanViewAuditLogs(c) {
		t.Fatal("ordinary viewer must not get audit log access")
	}
	c.Permissions = []string{"audit-logs:view"}
	if !CanViewAuditLogs(c) {
		t.Fatal("audit-logs:view should grant audit log access")
	}
	if !CanViewAuditLogs(&authmw.Claims{Sub: uuid.Nil, Roles: []string{"security-auditor"}}) {
		t.Fatal("security-auditor role should grant audit log access")
	}
}

func TestAuditDeliveryManagementRequiresDedicatedPermission(t *testing.T) {
	t.Parallel()
	c := &authmw.Claims{Sub: uuid.Nil, Roles: []string{"viewer"}, Permissions: []string{"audit-logs:view"}}
	if CanManageAuditDelivery(c) {
		t.Fatal("audit log viewers must not manage delivery destinations")
	}
	c.Permissions = append(c.Permissions, "audit-delivery:manage")
	if !CanManageAuditDelivery(c) {
		t.Fatal("audit-delivery:manage should grant delivery management")
	}
	if !CanManageAuditDelivery(&authmw.Claims{Sub: uuid.Nil, Roles: []string{"security-auditor"}}) {
		t.Fatal("security-auditor role should manage delivery destinations")
	}
}

func TestFilterEventsForClaimsKeepsAccessible(t *testing.T) {
	t.Parallel()
	c := claims("public")
	events := []models.AuditEvent{*event(models.ClassificationPublic), *event(models.ClassificationConfidential)}
	got := FilterEventsForClaims(events, c)
	if len(got) != 1 || got[0].Classification != string(models.ClassificationPublic) {
		t.Fatalf("filter should keep only public, got %v", got)
	}
}
