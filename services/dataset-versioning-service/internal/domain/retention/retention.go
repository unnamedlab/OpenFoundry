// Package retention ports the Foundry "Branch retention" doc resolver
// and eligibility check from Rust src/domain/retention.rs.
//
// The resolver is pure — it operates on already-loaded RetentionRow
// records — so the unit tests in this package exercise every edge case
// without a database. Three policies are recognised:
//
//   - FOREVER   — never archived. Used by `master` and any user-protected
//     long-lived branch.
//   - TTL_DAYS  — archived when last_activity_at is older than ttl_days,
//     the branch has no OPEN transaction, and is not a root.
//   - INHERITED — walks up parent_branch_id until it finds a branch with
//     FOREVER or TTL_DAYS. The default for new branches.
package retention

import (
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/dataset-versioning-service/internal/models"
)

// ParsePolicy mirrors Rust RetentionPolicy::parse.
func ParsePolicy(value string) (models.RetentionPolicy, bool) {
	switch value {
	case "INHERITED":
		return models.RetentionPolicyInherited, true
	case "FOREVER":
		return models.RetentionPolicyForever, true
	case "TTL_DAYS":
		return models.RetentionPolicyTTLDays, true
	default:
		return "", false
	}
}

// PolicyAsString mirrors Rust RetentionPolicy::as_str — returns the
// canonical SCREAMING_SNAKE_CASE label for serialisation/audit.
func PolicyAsString(p models.RetentionPolicy) string { return string(p) }

// DefaultForeverEffective is the Foundry-doc fallback when the parent
// chain bottoms out without anyone setting an explicit policy: keep the
// branch around. The doc is explicit that retention should never silently
// delete data — INHERITED with no explicit ancestor falls back to FOREVER.
func DefaultForeverEffective() models.EffectiveRetention {
	return models.EffectiveRetention{
		Policy:         models.RetentionPolicyForever,
		TTLDays:        nil,
		SourceBranchID: nil,
	}
}

// ResolveEffective resolves INHERITED against the row's parent chain.
// `index` MUST contain every ancestor by id (caller responsibility).
//
// Mirrors Rust resolve_effective_retention. A cycle in the parent
// chain — broken ancestry — returns DefaultForeverEffective() rather
// than spinning, matching Rust's defensive guard.
func ResolveEffective(row models.RetentionRow, index map[uuid.UUID]models.RetentionRow) models.EffectiveRetention {
	cursor := row
	visited := make([]uuid.UUID, 0, 8)
	for {
		for _, id := range visited {
			if id == cursor.ID {
				return DefaultForeverEffective()
			}
		}
		visited = append(visited, cursor.ID)
		switch cursor.Policy {
		case models.RetentionPolicyForever:
			id := cursor.ID
			return models.EffectiveRetention{
				Policy:         models.RetentionPolicyForever,
				TTLDays:        nil,
				SourceBranchID: &id,
			}
		case models.RetentionPolicyTTLDays:
			id := cursor.ID
			return models.EffectiveRetention{
				Policy:         models.RetentionPolicyTTLDays,
				TTLDays:        cursor.TTLDays,
				SourceBranchID: &id,
			}
		case models.RetentionPolicyInherited:
			if cursor.ParentBranchID == nil {
				return DefaultForeverEffective()
			}
			parent, ok := index[*cursor.ParentBranchID]
			if !ok {
				return DefaultForeverEffective()
			}
			cursor = parent
		default:
			return DefaultForeverEffective()
		}
	}
}

// IsArchiveEligible decides whether a branch is archive-eligible at `now`.
//
// Foundry guarantees:
//   - roots are never archived (ancestry would be lost),
//   - a branch with an OPEN transaction is never archived (in-flight
//     work would silently disappear),
//   - already-archived branches are skipped.
func IsArchiveEligible(row models.RetentionRow, effective models.EffectiveRetention, now time.Time) bool {
	if row.ArchivedAt != nil || row.IsRoot || row.HasOpenTransaction {
		return false
	}
	switch effective.Policy {
	case models.RetentionPolicyForever, models.RetentionPolicyInherited:
		return false
	case models.RetentionPolicyTTLDays:
		if effective.TTLDays == nil {
			return false
		}
		days := *effective.TTLDays
		if days <= 0 {
			return false
		}
		cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
		return row.LastActivityAt.Before(cutoff)
	default:
		return false
	}
}

// CountEligible returns the number of rows currently eligible for
// archive at `now`. Used by the worker for the gauge metric.
func CountEligible(rows []models.RetentionRow, index map[uuid.UUID]models.RetentionRow, now time.Time) int {
	n := 0
	for _, row := range rows {
		eff := ResolveEffective(row, index)
		if IsArchiveEligible(row, eff, now) {
			n++
		}
	}
	return n
}

// IndexRows builds the parent-chain lookup table used by the resolver.
func IndexRows(rows []models.RetentionRow) map[uuid.UUID]models.RetentionRow {
	out := make(map[uuid.UUID]models.RetentionRow, len(rows))
	for _, row := range rows {
		out[row.ID] = row
	}
	return out
}
