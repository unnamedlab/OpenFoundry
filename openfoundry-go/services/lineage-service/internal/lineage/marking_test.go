package lineage

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

func clearance(value string) *authmw.Claims {
	attrs, _ := json.Marshal(map[string]string{"classification_clearance": value})
	return &authmw.Claims{
		Sub:        uuid.New(),
		Roles:      []string{"operator"},
		Attributes: attrs,
	}
}

func TestMaxMarkingsPrefersStricter(t *testing.T) {
	t.Parallel()
	if got := MaxMarkingStrings("public", "confidential", "pii"); got != "pii" {
		t.Fatalf("got %s", got)
	}
	if got := MaxMarkingStrings("public", "confidential"); got != "confidential" {
		t.Fatalf("got %s", got)
	}
	if got := MaxMarkingStrings(); got != "public" {
		t.Fatalf("empty got %s", got)
	}
}

func TestNormalizeMarking(t *testing.T) {
	t.Parallel()
	if got := NormalizeMarking(nil); got == nil || *got != "public" {
		t.Fatalf("nil got %v", got)
	}
	weird := "weird"
	if got := NormalizeMarking(&weird); got != nil {
		t.Fatalf("weird should normalize to nil, got %v", got)
	}
	pii := "pii"
	if got := NormalizeMarking(&pii); got == nil || *got != "pii" {
		t.Fatalf("pii got %v", got)
	}
}

func TestMarkingFromDatasetTagsHonoursPrefixes(t *testing.T) {
	t.Parallel()
	got := MarkingFromDatasetTags([]string{"team:platform", "marking:confidential"})
	if got != "confidential" {
		t.Fatalf("got %s", got)
	}
	got = MarkingFromDatasetTags([]string{"classification:pii"})
	if got != "pii" {
		t.Fatalf("got %s", got)
	}
	got = MarkingFromDatasetTags([]string{"PII"})
	if got != "pii" {
		t.Fatalf("got %s", got)
	}
	got = MarkingFromDatasetTags([]string{"random"})
	if got != "public" {
		t.Fatalf("got %s", got)
	}
}

func TestRequiresMarkingAcknowledgement(t *testing.T) {
	t.Parallel()
	if RequiresMarkingAcknowledgement("public") {
		t.Fatal("public should not require ack")
	}
	if !RequiresMarkingAcknowledgement("confidential") {
		t.Fatal("confidential should require ack")
	}
	if !RequiresMarkingAcknowledgement("pii") {
		t.Fatal("pii should require ack")
	}
}

func TestCanAccessMarkingHonoursClearance(t *testing.T) {
	t.Parallel()
	c := clearance("confidential")
	if !CanAccessMarking(c, "public") {
		t.Fatal("confidential clearance should see public")
	}
	if !CanAccessMarking(c, "confidential") {
		t.Fatal("confidential clearance should see confidential")
	}
	if CanAccessMarking(c, "pii") {
		t.Fatal("confidential clearance must NOT see pii")
	}
}

func TestCanAccessMarkingAdminBypass(t *testing.T) {
	t.Parallel()
	admin := &authmw.Claims{Sub: uuid.New(), Roles: []string{"admin"}}
	if !CanAccessMarking(admin, "pii") {
		t.Fatal("admin should pass pii")
	}
}
