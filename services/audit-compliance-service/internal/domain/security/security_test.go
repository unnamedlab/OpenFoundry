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
		Sub:        uuid.Nil,
		Roles:      []string{"viewer"},
		Attributes: attrs,
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

func TestFilterEventsForClaimsKeepsAccessible(t *testing.T) {
	t.Parallel()
	c := claims("public")
	events := []models.AuditEvent{*event(models.ClassificationPublic), *event(models.ClassificationConfidential)}
	got := FilterEventsForClaims(events, c)
	if len(got) != 1 || got[0].Classification != string(models.ClassificationPublic) {
		t.Fatalf("filter should keep only public, got %v", got)
	}
}
