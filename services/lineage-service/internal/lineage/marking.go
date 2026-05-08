package lineage

import (
	"strings"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// MarkingRank ports `marking_rank` from Rust. Higher = stricter.
//
//	pii          → 2
//	confidential → 1
//	anything else→ 0
func MarkingRank(marking string) uint8 {
	switch marking {
	case "pii":
		return 2
	case "confidential":
		return 1
	default:
		return 0
	}
}

// NormalizeMarking ports `normalize_marking`. Returns nil for unknown
// values (so callers can fall back to a default), and "public" when
// the input is nil or empty.
func NormalizeMarking(marking *string) *string {
	value := "public"
	if marking != nil {
		value = *marking
	}
	switch value {
	case "public":
		out := "public"
		return &out
	case "confidential":
		out := "confidential"
		return &out
	case "pii":
		out := "pii"
		return &out
	default:
		return nil
	}
}

// MaxMarkings returns the strictest marking from the list. Nil
// pointers and unrecognised values count as "public" (rank 0).
func MaxMarkings(markings []*string) string {
	best := "public"
	bestRank := uint8(0)
	for _, candidate := range markings {
		v := "public"
		if candidate != nil {
			v = *candidate
		}
		if r := MarkingRank(v); r > bestRank {
			best = v
			bestRank = r
		}
	}
	return best
}

// MaxMarkingStrings is the convenience overload taking string values
// directly (an empty string is treated as "public", same as the Rust
// `Option<&str>::None` arm).
func MaxMarkingStrings(values ...string) string {
	ptrs := make([]*string, 0, len(values))
	for i := range values {
		v := values[i]
		ptrs = append(ptrs, &v)
	}
	return MaxMarkings(ptrs)
}

// RequiresMarkingAcknowledgement is true for anything stricter than
// public.
func RequiresMarkingAcknowledgement(marking string) bool {
	return MarkingRank(marking) > 0
}

// MarkingFromDatasetTags ports `marking_from_dataset_tags`. Honours
// the `marking:` and `classification:` prefixes first, then falls
// back to a tag-name heuristic, then "public".
func MarkingFromDatasetTags(tags []string) string {
	for _, prefix := range []string{"marking:", "classification:"} {
		for _, tag := range tags {
			if !strings.HasPrefix(tag, prefix) {
				continue
			}
			candidate := strings.TrimPrefix(tag, prefix)
			if normalized := NormalizeMarking(&candidate); normalized != nil {
				return *normalized
			}
		}
	}
	for _, tag := range tags {
		if strings.EqualFold(tag, "pii") {
			return "pii"
		}
	}
	for _, tag := range tags {
		if strings.EqualFold(tag, "confidential") {
			return "confidential"
		}
	}
	return "public"
}

// ClearanceRank reads `attributes.classification_clearance` and maps
// it through MarkingRank. Mirrors the Rust helper exactly: missing or
// non-string clearance yields rank 0.
func ClearanceRank(claims *authmw.Claims) uint8 {
	if claims == nil {
		return 0
	}
	clearance, ok := claims.ClassificationClearance()
	if !ok {
		return 0
	}
	return MarkingRank(clearance)
}

// CanAccessMarking is the Foundry-style classification gate.
//
// Admins always pass; otherwise the caller's clearance rank must be
// at least the marking rank.
func CanAccessMarking(claims *authmw.Claims, marking string) bool {
	if claims == nil {
		return MarkingRank(marking) == 0
	}
	if claims.HasRole("admin") {
		return true
	}
	return ClearanceRank(claims) >= MarkingRank(marking)
}
