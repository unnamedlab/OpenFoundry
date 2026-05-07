// Package immutablelog ports
// `services/audit-compliance-service/src/domain/immutable_log.rs` 1:1.
//
// Provides the hash-chain primitives used by `events::persist_event`:
//
//   - NextSequence  — monotonic int64 sequence
//   - PreviousHashValue — defaults to "GENESIS" when the chain is empty
//   - ChainHash — `AUD-{seq:08x}-{prevhash[:8]}-{src-action[:8]}`
//   - LabelEvent — appends `contains-sensitive-data`, `gdpr-subject-linked`
//
// Pure no-IO; safe to call from tests.
package immutablelog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// NextSequence mirrors `next_sequence`. Returns `previous + 1`, treating
// nil (no previous row) as 0.
func NextSequence(previousSequence *int64) int64 {
	if previousSequence == nil {
		return 1
	}
	return *previousSequence + 1
}

// PreviousHashValue mirrors `previous_hash_value`. Returns `"GENESIS"`
// when the chain is empty.
func PreviousHashValue(previousHash *string) string {
	if previousHash == nil {
		return "GENESIS"
	}
	return *previousHash
}

// ChainHash mirrors `chain_hash`. Same format string as the Rust impl
// so existing rows verify after cutover.
func ChainHash(sequence int64, previousHash, sourceService, action string) string {
	return fmt.Sprintf("AUD-%08x-%s-%s",
		sequence,
		normalize(previousHash),
		normalize(sourceService+"-"+action))
}

// LabelEvent mirrors `label_event`. Appends `contains-sensitive-data`
// when the event is masked-required, and `gdpr-subject-linked` when a
// subject_id is set. Returns a sorted, deduplicated copy.
func LabelEvent(event *models.AuditEvent, baseLabels []string) ([]string, error) {
	labels := append([]string(nil), baseLabels...)

	classification, err := models.ParseClassificationLevel(event.Classification)
	if err == nil && classification.RequiresMasking() {
		labels = append(labels, "contains-sensitive-data")
	}
	if event.SubjectID != nil && *event.SubjectID != "" {
		labels = append(labels, "gdpr-subject-linked")
	}
	return sortedUnique(labels), nil
}

// SortedUniqueLabels exposes the dedup helper for callers that want
// to apply it to a fresh label set without going through LabelEvent.
func SortedUniqueLabels(labels []string) []string { return sortedUnique(labels) }

func sortedUnique(labels []string) []string {
	if len(labels) == 0 {
		return nil
	}
	sort.Strings(labels)
	out := make([]string, 0, len(labels))
	prev := ""
	for i, label := range labels {
		if i == 0 || label != prev {
			out = append(out, label)
		}
		prev = label
	}
	return out
}

// normalize mirrors `normalize` — keeps ASCII alphanumerics, takes the
// first 8, uppercases the result.
func normalize(value string) string {
	var b strings.Builder
	b.Grow(8)
	count := 0
	for _, r := range value {
		if count == 8 {
			break
		}
		switch {
		case r >= '0' && r <= '9',
			r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z':
			b.WriteRune(r)
			count++
		}
	}
	return strings.ToUpper(b.String())
}
