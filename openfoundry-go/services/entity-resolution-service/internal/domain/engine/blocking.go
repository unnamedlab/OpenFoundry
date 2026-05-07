package engine

import (
	"fmt"
	"sort"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

// CandidatePair mirrors fusion_base::domain::engine::blocking::CandidatePair.
type CandidatePair struct {
	Left        models.EntityRecord
	Right       models.EntityRecord
	BlockingKey string
}

// BuildCandidatePairs ports `blocking::build_candidate_pairs`.
func BuildCandidatePairs(records []models.EntityRecord, strategy models.BlockingStrategyConfig) []CandidatePair {
	switch strategy.StrategyType {
	case "sorted-neighborhood":
		return sortedNeighborhoodPairs(records, strategy)
	case "lsh":
		return lshPairs(records, strategy)
	default:
		return keyBasedPairs(records, strategy)
	}
}

func keyBasedPairs(records []models.EntityRecord, strategy models.BlockingStrategyConfig) []CandidatePair {
	groups := map[string][]int{}
	for i, record := range records {
		key := blockingKey(record, strategy.KeyFields)
		groups[key] = append(groups[key], i)
	}

	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	emitted := map[string]struct{}{}
	pairs := []CandidatePair{}
	for _, key := range keys {
		group := groups[key]
		for li := 0; li < len(group); li++ {
			for ri := li + 1; ri < len(group); ri++ {
				pairs = emitPair(pairs, emitted, key, records[group[li]], records[group[ri]])
			}
		}
	}
	return pairs
}

func sortedNeighborhoodPairs(records []models.EntityRecord, strategy models.BlockingStrategyConfig) []CandidatePair {
	type keyed struct {
		key string
		rec models.EntityRecord
	}
	sorted := make([]keyed, len(records))
	for i, r := range records {
		sorted[i] = keyed{key: blockingKey(r, strategy.KeyFields), rec: r}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].key < sorted[j].key })

	windowSize := int(strategy.WindowSize)
	if windowSize < 2 {
		windowSize = 2
	}
	emitted := map[string]struct{}{}
	pairs := []CandidatePair{}
	for li := 0; li < len(sorted); li++ {
		upper := li + windowSize
		if upper > len(sorted) {
			upper = len(sorted)
		}
		for ri := li + 1; ri < upper; ri++ {
			combinedKey := fmt.Sprintf("%s|%s", sorted[li].key, sorted[ri].key)
			pairs = emitPair(pairs, emitted, combinedKey, sorted[li].rec, sorted[ri].rec)
		}
	}
	return pairs
}

func lshPairs(records []models.EntityRecord, strategy models.BlockingStrategyConfig) []CandidatePair {
	bucketCount := uint64(strategy.BucketCount)
	if bucketCount < 4 {
		bucketCount = 4
	}
	buckets := map[string][]int{}
	for i, record := range records {
		for _, bucket := range recordBuckets(record, bucketCount) {
			buckets[bucket] = append(buckets[bucket], i)
		}
	}
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	emitted := map[string]struct{}{}
	pairs := []CandidatePair{}
	for _, bucket := range keys {
		group := buckets[bucket]
		for li := 0; li < len(group); li++ {
			for ri := li + 1; ri < len(group); ri++ {
				pairs = emitPair(pairs, emitted, bucket, records[group[li]], records[group[ri]])
			}
		}
	}
	return pairs
}

func recordBuckets(record models.EntityRecord, bucketCount uint64) []string {
	seed := NormalizeText(record.DisplayName)
	tokens := tokenize(seed)
	bucketSet := map[string]struct{}{}
	for _, token := range tokens {
		if token == "" {
			continue
		}
		var hash uint64
		for _, b := range []byte(token) {
			hash = hash*31 + uint64(b)
		}
		bucketSet[fmt.Sprintf("bucket-%d", hash%bucketCount)] = struct{}{}
	}
	if len(bucketSet) == 0 {
		bucketSet["bucket-0"] = struct{}{}
	}
	out := make([]string, 0, len(bucketSet))
	for k := range bucketSet {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func tokenize(input string) []string {
	out := []string{}
	current := []rune{}
	for _, r := range input {
		switch r {
		case ' ', '\t', '\n', '\r':
			if len(current) > 0 {
				out = append(out, string(current))
				current = current[:0]
			}
		default:
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		out = append(out, string(current))
	}
	return out
}

func emitPair(pairs []CandidatePair, emitted map[string]struct{}, blockingKey string, left, right models.EntityRecord) []CandidatePair {
	key := pairKey(left.RecordID, right.RecordID)
	if _, ok := emitted[key]; ok {
		return pairs
	}
	emitted[key] = struct{}{}
	return append(pairs, CandidatePair{Left: left, Right: right, BlockingKey: blockingKey})
}

func pairKey(left, right string) string {
	if left <= right {
		return left + "::" + right
	}
	return right + "::" + left
}

func blockingKey(record models.EntityRecord, keyFields []string) string {
	parts := []string{}
	for _, field := range keyFields {
		raw, ok := extractFieldString(record, field)
		if !ok {
			continue
		}
		normalized := NormalizeText(raw)
		if normalized == "" {
			continue
		}
		parts = append(parts, takeRunes(normalized, 6))
	}
	if len(parts) == 0 {
		parts = append(parts, takeRunes(NormalizeText(record.DisplayName), 6))
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "|"
		}
		out += p
	}
	return out
}

func extractFieldString(record models.EntityRecord, field string) (string, bool) {
	switch field {
	case "display_name", "name":
		return record.DisplayName, true
	default:
		v, ok := record.Attributes[field]
		if !ok {
			return "", false
		}
		s, ok := v.(string)
		if !ok {
			return "", false
		}
		return s, true
	}
}

func takeRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
