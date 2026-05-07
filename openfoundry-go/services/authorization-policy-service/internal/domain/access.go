// Package domain holds shared domain helpers (markings, ABAC evaluator).
package domain

import (
	"fmt"
	"sort"
	"strings"
)

// ValidMarkings mirrors the Rust constant — accepted classification labels.
var ValidMarkings = []string{"public", "confidential", "pii"}

// ValidateMarking returns an error when `m` isn't one of [ValidMarkings].
func ValidateMarking(m string) error {
	for _, v := range ValidMarkings {
		if v == m {
			return nil
		}
	}
	return fmt.Errorf("invalid marking '%s', valid markings: %v", m, ValidMarkings)
}

// MarkingRank ranks classification labels (lowest → highest sensitivity).
// Returns -1 for unknown labels (matches the Rust Option<u8> None case).
func MarkingRank(m string) int {
	switch m {
	case "public":
		return 0
	case "confidential":
		return 1
	case "pii":
		return 2
	}
	return -1
}

// NormalizeMarkings lowercases, trims, dedups, and validates an input list.
func NormalizeMarkings(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	out = dedupSorted(out)
	for _, m := range out {
		if err := ValidateMarking(m); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// MaxMarking returns the highest-ranked marking in the input list.
func MaxMarking(values []string) (string, bool) {
	bestRank := -1
	best := ""
	for _, v := range values {
		r := MarkingRank(v)
		if r > bestRank {
			bestRank = r
			best = v
		}
	}
	if bestRank < 0 {
		return "", false
	}
	return best, true
}

// MarkingsForClearance returns the set of markings a clearance label
// permits. Empty / unknown clearance → ["public"] only.
func MarkingsForClearance(clearance string) []string {
	rank := MarkingRank(clearance)
	if rank < 0 {
		rank = 0
	}
	out := make([]string, 0, len(ValidMarkings))
	for _, m := range ValidMarkings {
		if MarkingRank(m) <= rank {
			out = append(out, m)
		}
	}
	return out
}

func dedupSorted(s []string) []string {
	if len(s) <= 1 {
		return s
	}
	w := 1
	for i := 1; i < len(s); i++ {
		if s[i] != s[w-1] {
			s[w] = s[i]
			w++
		}
	}
	return s[:w]
}
