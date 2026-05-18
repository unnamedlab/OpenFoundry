package domain

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestBranchStatus_IsTerminal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status BranchStatus
		want   bool
	}{
		{StatusOpen, false},
		{StatusMerging, false},
		{StatusStale, false},
		{StatusMerged, true},
		{StatusAbandoned, true},
	}
	for _, tc := range cases {
		if got := tc.status.IsTerminal(); got != tc.want {
			t.Errorf("IsTerminal(%q)=%v want %v", tc.status, got, tc.want)
		}
	}
}

func TestGlobalBranch_ValidateNew(t *testing.T) {
	t.Parallel()
	tenant := uuid.New()
	t.Run("missing tenant", func(t *testing.T) {
		b := &GlobalBranch{Name: "release-q3", BaseRef: "main"}
		if err := b.ValidateNew(); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("missing name", func(t *testing.T) {
		b := &GlobalBranch{TenantID: tenant, BaseRef: "main"}
		if err := b.ValidateNew(); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("missing base ref", func(t *testing.T) {
		b := &GlobalBranch{TenantID: tenant, Name: "x"}
		if err := b.ValidateNew(); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("defaults status to open", func(t *testing.T) {
		b := &GlobalBranch{TenantID: tenant, Name: "release-q3", BaseRef: "main"}
		if err := b.ValidateNew(); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if b.Status != StatusOpen {
			t.Fatalf("status=%q want %q", b.Status, StatusOpen)
		}
	})
	t.Run("trims name", func(t *testing.T) {
		b := &GlobalBranch{TenantID: tenant, Name: "  release-q3 ", BaseRef: " main "}
		if err := b.ValidateNew(); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
		if b.Name != "release-q3" {
			t.Fatalf("name=%q", b.Name)
		}
	})
	t.Run("rejects unknown status", func(t *testing.T) {
		b := &GlobalBranch{TenantID: tenant, Name: "x", BaseRef: "main", Status: "wat"}
		if err := b.ValidateNew(); !errors.Is(err, ErrInvalidStatus) {
			t.Fatalf("err=%v want ErrInvalidStatus", err)
		}
	})
}

func TestGlobalBranch_CanAcceptParticipation_RejectsMerged(t *testing.T) {
	t.Parallel()
	b := &GlobalBranch{Status: StatusMerged}
	if err := b.CanAcceptParticipation(); !errors.Is(err, ErrBranchClosed) {
		t.Fatalf("err=%v want ErrBranchClosed", err)
	}
	b.Status = StatusAbandoned
	if err := b.CanAcceptParticipation(); !errors.Is(err, ErrBranchClosed) {
		t.Fatalf("abandoned: err=%v want ErrBranchClosed", err)
	}
	for _, s := range []BranchStatus{StatusOpen, StatusMerging, StatusStale} {
		b.Status = s
		if err := b.CanAcceptParticipation(); err != nil {
			t.Fatalf("status=%s err=%v want nil", s, err)
		}
	}
}

func TestGlobalBranch_CanMerge(t *testing.T) {
	t.Parallel()
	open := &GlobalBranch{Status: StatusOpen}
	if err := open.CanMerge(nil); err != nil {
		t.Fatalf("open with no parts: %v", err)
	}
	parts := []Participation{
		{ServiceName: "ontology", Status: ParticipationActive},
		{ServiceName: "datasets", Status: ParticipationConflict},
	}
	if err := open.CanMerge(parts); !errors.Is(err, ErrCannotMergeWithConflicts) {
		t.Fatalf("err=%v want ErrCannotMergeWithConflicts", err)
	}
	merged := &GlobalBranch{Status: StatusMerged}
	if err := merged.CanMerge(nil); !errors.Is(err, ErrBranchClosed) {
		t.Fatalf("merged: err=%v want ErrBranchClosed", err)
	}
}
