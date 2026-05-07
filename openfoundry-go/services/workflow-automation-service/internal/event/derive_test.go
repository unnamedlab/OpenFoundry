package event

import (
	"testing"

	"github.com/google/uuid"
)

func TestDeriveRunIDIsStable(t *testing.T) {
	t.Parallel()
	d := uuid.New()
	c := uuid.New()
	a := DeriveRunID(d, c)
	b := DeriveRunID(d, c)
	if a != b {
		t.Fatalf("expected stable, got %s vs %s", a, b)
	}
	if a.Version() != 5 {
		t.Fatalf("expected v5, got v%d", a.Version())
	}
}

func TestDeriveRunIDDiffersPerInputPair(t *testing.T) {
	t.Parallel()
	d1, d2 := uuid.New(), uuid.New()
	c := uuid.New()
	if DeriveRunID(d1, c) == DeriveRunID(d2, c) {
		t.Fatal("different definition_id should produce different run_id")
	}
	c2 := uuid.New()
	if DeriveRunID(d1, c) == DeriveRunID(d1, c2) {
		t.Fatal("different correlation_id should produce different run_id")
	}
}

func TestConditionEventIDIsDistinctFromRunID(t *testing.T) {
	t.Parallel()
	d := uuid.New()
	c := uuid.New()
	if DeriveRunID(d, c) == DeriveConditionEventID(d, c) {
		t.Fatal("event id must not collide with run id")
	}
	if DeriveConditionEventID(d, c).Version() != 5 {
		t.Fatal("expected v5")
	}
}

func TestTenantUUIDFromStrRoundTripsUUIDs(t *testing.T) {
	t.Parallel()
	in := uuid.New()
	if got := TenantUUIDFromStr(in.String()); got != in {
		t.Fatalf("expected %s, got %s", in, got)
	}
}

func TestTenantUUIDFromStrIsStableForArbitraryStrings(t *testing.T) {
	t.Parallel()
	a := TenantUUIDFromStr("acme-corp")
	b := TenantUUIDFromStr("acme-corp")
	c := TenantUUIDFromStr("acme-corp-2")
	if a != b {
		t.Fatal("must be stable")
	}
	if a == c {
		t.Fatal("different inputs must produce different ids")
	}
	if a.Version() != 5 {
		t.Fatal("expected v5")
	}
}
