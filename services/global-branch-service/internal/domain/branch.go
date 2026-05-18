// Package domain holds the pure types and lifecycle rules for the
// global-branch-service. The HTTP and repo layers depend on this
// package; this package depends only on the standard library so it
// can be exercised by table-driven tests without spinning up a DB.
package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// BranchStatus is the lifecycle state of a global branch.
//
// Transitions allowed at Milestone A:
//
//	open    -> merging | abandoned | stale
//	merging -> merged | open (on merge failure)
//	merged  -> (terminal)
//	abandoned -> (terminal)
//	stale   -> abandoned
type BranchStatus string

const (
	StatusOpen      BranchStatus = "open"
	StatusMerging   BranchStatus = "merging"
	StatusMerged    BranchStatus = "merged"
	StatusAbandoned BranchStatus = "abandoned"
	StatusStale     BranchStatus = "stale"
)

// IsValid reports whether s is one of the recognised statuses.
func (s BranchStatus) IsValid() bool {
	switch s {
	case StatusOpen, StatusMerging, StatusMerged, StatusAbandoned, StatusStale:
		return true
	}
	return false
}

// IsTerminal reports whether the branch can no longer accept
// lifecycle mutations (participations, merges, etc.).
func (s BranchStatus) IsTerminal() bool {
	return s == StatusMerged || s == StatusAbandoned
}

// ParticipationStatus is the per-service status of a service's
// participation in a global branch.
type ParticipationStatus string

const (
	ParticipationPending  ParticipationStatus = "pending"
	ParticipationActive   ParticipationStatus = "active"
	ParticipationMerged   ParticipationStatus = "merged"
	ParticipationConflict ParticipationStatus = "conflict"
)

// IsValid reports whether s is a recognised participation status.
func (s ParticipationStatus) IsValid() bool {
	switch s {
	case ParticipationPending, ParticipationActive, ParticipationMerged, ParticipationConflict:
		return true
	}
	return false
}

// GlobalBranch is a cross-application branch coordinating local
// branches across multiple services (Ontology, Datasets, Workshop,
// Pipelines).
type GlobalBranch struct {
	ID                    uuid.UUID    `json:"id"`
	TenantID              uuid.UUID    `json:"tenant_id"`
	Name                  string       `json:"name"`
	BaseRef               string       `json:"base_ref"`
	Status                BranchStatus `json:"status"`
	Description           string       `json:"description,omitempty"`
	CreatedBy             uuid.UUID    `json:"created_by"`
	CreatedAt             time.Time    `json:"created_at"`
	MergedAt              *time.Time   `json:"merged_at,omitempty"`
	MergedBy              *uuid.UUID   `json:"merged_by,omitempty"`
	ParticipatingServices []string     `json:"participating_services"`
}

// Participation is one service's enrolment in a global branch. The
// local branch ref names the matching branch on that service's
// per-application branching surface (e.g. a dataset branch name or a
// code-repo branch ref).
type Participation struct {
	GlobalBranchID uuid.UUID           `json:"global_branch_id"`
	ServiceName    string              `json:"service_name"`
	LocalBranchRef string              `json:"local_branch_ref"`
	Status         ParticipationStatus `json:"status"`
	LastSyncedAt   *time.Time          `json:"last_synced_at,omitempty"`
}

// ── Sentinels ────────────────────────────────────────────────────────

// ErrBranchNotFound is returned when the addressed branch row does not
// exist. HTTP layer maps to 404.
var ErrBranchNotFound = errors.New("global-branch: branch not found")

// ErrBranchClosed is returned when a write is attempted against a
// branch that is in a terminal state (merged / abandoned). HTTP layer
// maps to 409 Conflict.
var ErrBranchClosed = errors.New("global-branch: branch is closed")

// ErrParticipationExists is returned when a service tries to register
// a second participation on the same global branch. HTTP layer maps to
// 409 Conflict — the right move is to PATCH the existing row.
var ErrParticipationExists = errors.New("global-branch: service already participates in this branch")

// ErrCannotMergeWithConflicts is returned when a merge is requested
// while any participation is in `conflict` state. HTTP layer maps to
// 409 Conflict.
var ErrCannotMergeWithConflicts = errors.New("global-branch: cannot merge while a participation is in conflict")

// ErrInvalidStatus signals a write that would land the branch in a
// status outside the recognised set, or a transition that the
// lifecycle rules forbid (e.g. abandoning a merged branch). HTTP
// layer maps to 422.
var ErrInvalidStatus = errors.New("global-branch: invalid branch status transition")

// ── Lifecycle helpers ────────────────────────────────────────────────

// ValidateNew runs the pre-create invariants. The repo layer is the
// authority on uniqueness; this function checks shape-level rules
// (name non-empty, base ref non-empty, tenant set).
func (b *GlobalBranch) ValidateNew() error {
	if b.TenantID == uuid.Nil {
		return errors.New("global-branch: tenant_id required")
	}
	name := strings.TrimSpace(b.Name)
	if name == "" {
		return errors.New("global-branch: name required")
	}
	if strings.TrimSpace(b.BaseRef) == "" {
		return errors.New("global-branch: base_ref required")
	}
	if b.Status == "" {
		b.Status = StatusOpen
	}
	if !b.Status.IsValid() {
		return ErrInvalidStatus
	}
	b.Name = name
	b.Description = strings.TrimSpace(b.Description)
	return nil
}

// CanAcceptParticipation reports whether the branch is in a state that
// allows new service enrolment. Returns ErrBranchClosed when the
// branch is terminal.
//
// The unit test in domain_test.go pins the rule that participations
// cannot be added to a merged branch.
func (b *GlobalBranch) CanAcceptParticipation() error {
	if b.Status.IsTerminal() {
		return ErrBranchClosed
	}
	return nil
}

// CanMerge reports whether the merge endpoint should proceed. It
// rejects terminal branches with ErrBranchClosed and merges against
// branches with any `conflict` participation with
// ErrCannotMergeWithConflicts. The caller passes the latest
// participation snapshot so the check is consistent with what's in
// the DB inside the calling transaction.
func (b *GlobalBranch) CanMerge(parts []Participation) error {
	if b.Status.IsTerminal() {
		return ErrBranchClosed
	}
	for _, p := range parts {
		if p.Status == ParticipationConflict {
			return ErrCannotMergeWithConflicts
		}
	}
	return nil
}
