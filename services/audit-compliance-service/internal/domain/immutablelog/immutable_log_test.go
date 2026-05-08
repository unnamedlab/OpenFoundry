package immutablelog

import (
	"strings"
	"testing"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

func TestNextSequenceFromGenesis(t *testing.T) {
	t.Parallel()
	if got := NextSequence(nil); got != 1 {
		t.Fatalf("nil → %d, want 1", got)
	}
	five := int64(5)
	if got := NextSequence(&five); got != 6 {
		t.Fatalf("Some(5) → %d, want 6", got)
	}
}

func TestPreviousHashGenesisDefault(t *testing.T) {
	t.Parallel()
	if got := PreviousHashValue(nil); got != "GENESIS" {
		t.Fatalf("nil → %s, want GENESIS", got)
	}
	prev := "PREV"
	if got := PreviousHashValue(&prev); got != "PREV" {
		t.Fatalf("got %s", got)
	}
}

func TestChainHashShape(t *testing.T) {
	t.Parallel()
	got := ChainHash(7, "GENESIS", "gateway", "user-login")
	if !strings.HasPrefix(got, "AUD-00000007-") {
		t.Fatalf("missing AUD-00000007 prefix: %s", got)
	}
	parts := strings.Split(got, "-")
	if len(parts) != 4 {
		t.Fatalf("expected 4 dash-segments, got %d (%s)", len(parts), got)
	}
	for _, p := range parts[1:] {
		if strings.ToUpper(p) != p {
			t.Fatalf("segment %q must be uppercase", p)
		}
	}
}

func TestLabelEventAppendsContainsSensitiveDataForPii(t *testing.T) {
	t.Parallel()
	event := models.AuditEvent{Classification: string(models.ClassificationPii), SubjectID: nil}
	labels, err := LabelEvent(&event, []string{"x"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	got := strings.Join(labels, ",")
	if !strings.Contains(got, "contains-sensitive-data") {
		t.Fatalf("expected contains-sensitive-data in %v", labels)
	}
	if strings.Contains(got, "gdpr-subject-linked") {
		t.Fatalf("subject-linked should not be present: %v", labels)
	}
}

func TestLabelEventAppendsBothFlagsAndDeduplicates(t *testing.T) {
	t.Parallel()
	subject := "subject-1"
	event := models.AuditEvent{
		Classification: string(models.ClassificationConfidential),
		SubjectID:      &subject,
	}
	labels, err := LabelEvent(&event, []string{"contains-sensitive-data", "z"})
	if err != nil {
		t.Fatal(err)
	}
	if labels[0] != "contains-sensitive-data" {
		t.Fatalf("expected sorted output, got %v", labels)
	}
	count := 0
	for _, l := range labels {
		if l == "contains-sensitive-data" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("dedup failed, count=%d", count)
	}
}
