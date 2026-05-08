package automationoperations

import (
	"testing"

	"github.com/google/uuid"
)

func TestDeriveSagaIDIsStable(t *testing.T) {
	t.Parallel()
	c := uuid.Nil
	a := DeriveSagaID("retention.sweep", c)
	b := DeriveSagaID("retention.sweep", c)
	if a != b {
		t.Fatal("must be stable")
	}
	if a.Version() != 5 {
		t.Fatal("expected v5")
	}
}

func TestDeriveSagaIDDiffersPerInput(t *testing.T) {
	t.Parallel()
	c1 := uuid.New()
	c2 := uuid.New()
	if DeriveSagaID("retention.sweep", c1) == DeriveSagaID("retention.sweep", c2) {
		t.Fatal("different correlation_id should produce different saga_id")
	}
	if DeriveSagaID("retention.sweep", c1) == DeriveSagaID("cleanup.workspace", c1) {
		t.Fatal("different task_type should produce different saga_id")
	}
}

func TestRequestEventIDDistinctFromSagaID(t *testing.T) {
	t.Parallel()
	c := uuid.New()
	if DeriveSagaID("retention.sweep", c) == DeriveRequestEventID("retention.sweep", c) {
		t.Fatal("event id must not collide with saga id")
	}
	if DeriveRequestEventID("retention.sweep", c).Version() != 5 {
		t.Fatal("expected v5")
	}
}

func TestKnownSagaTypes(t *testing.T) {
	t.Parallel()
	if !IsKnown("retention.sweep") {
		t.Fatal("retention.sweep should be known")
	}
	if !IsKnown("cleanup.workspace") {
		t.Fatal("cleanup.workspace should be known")
	}
	if IsKnown("does-not-exist") {
		t.Fatal("unknown saga should be rejected")
	}
	if IsKnown("") {
		t.Fatal("empty saga should be rejected")
	}
}
