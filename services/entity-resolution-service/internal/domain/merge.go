package domain

import (
	"sort"
	"strings"
	"time"

	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/domain/engine"
	"github.com/openfoundry/openfoundry-go/services/entity-resolution-service/internal/models"
)

// SynthesizeGoldenRecord ports `merge::synthesize_golden_record`.
func SynthesizeGoldenRecord(cluster models.ResolvedCluster, strategy models.MergeStrategy) models.GoldenRecord {
	now := time.Now().UTC()

	fieldRules := strategy.Rules
	if len(fieldRules) == 0 {
		fieldRules = defaultSurvivorshipRules()
	}

	canonicalValues := map[string]any{}
	provenance := []models.GoldenRecordProvenance{}

	for _, rule := range fieldRules {
		selected, ok := selectValue(cluster.Records, rule, strategy.DefaultStrategy)
		if !ok {
			continue
		}
		canonicalValues[rule.Field] = selected.value
		provenance = append(provenance, models.GoldenRecordProvenance{
			Field:      rule.Field,
			Source:     selected.source,
			ExternalID: selected.externalID,
			Strategy:   selected.strategy,
		})
	}

	if _, ok := canonicalValues["display_name"]; !ok {
		if len(cluster.Records) > 0 {
			canonicalValues["display_name"] = cluster.Records[0].DisplayName
		}
	}

	title := "Golden Record"
	if v, ok := canonicalValues["display_name"]; ok {
		if s, ok := v.(string); ok {
			title = s
		}
	} else if v, ok := canonicalValues["name"]; ok {
		if s, ok := v.(string); ok {
			title = s
		}
	}

	denominator := len(fieldRules)
	if denominator < 1 {
		denominator = 1
	}
	completeness := float32(len(canonicalValues)) / float32(denominator)
	switch {
	case completeness < 0:
		completeness = 0
	case completeness > 1:
		completeness = 1
	}

	status := "active"
	if cluster.Status == "rejected" {
		status = "rejected"
	}

	return models.GoldenRecord{
		ID:                engine.MustNewUUIDv7(),
		ClusterID:         cluster.ID,
		Title:             title,
		CanonicalValues:   canonicalValues,
		Provenance:        provenance,
		CompletenessScore: completeness,
		ConfidenceScore:   cluster.ConfidenceScore,
		Status:            status,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

type selectedValue struct {
	value      any
	source     string
	externalID string
	strategy   string
}

func selectValue(records []models.EntityRecord, rule models.SurvivorshipRule, defaultStrategy string) (selectedValue, bool) {
	strategy := strings.TrimSpace(rule.Strategy)
	if strategy == "" {
		strategy = defaultStrategy
	}
	switch strategy {
	case "source_priority":
		return selectSourcePriority(records, rule)
	case "highest_confidence":
		return selectHighestConfidence(records, rule.Field, strategy)
	case "most_common":
		return selectMostCommon(records, rule.Field, strategy)
	default:
		return selectLongestNonEmpty(records, rule.Field, strategy)
	}
}

func selectSourcePriority(records []models.EntityRecord, rule models.SurvivorshipRule) (selectedValue, bool) {
	for _, source := range rule.SourcePriority {
		for _, record := range records {
			if record.Source != source {
				continue
			}
			value, ok := extractValue(record, rule.Field)
			if !ok {
				continue
			}
			return selectedValue{
				value:      value,
				source:     record.Source,
				externalID: record.ExternalID,
				strategy:   "source_priority",
			}, true
		}
	}
	return selectLongestNonEmpty(records, rule.Field, rule.Fallback)
}

func selectHighestConfidence(records []models.EntityRecord, field, strategy string) (selectedValue, bool) {
	var best *models.EntityRecord
	var bestValue any
	for i := range records {
		value, ok := extractValue(records[i], field)
		if !ok {
			continue
		}
		if best == nil || records[i].Confidence > best.Confidence {
			best = &records[i]
			bestValue = value
		}
	}
	if best == nil {
		return selectedValue{}, false
	}
	return selectedValue{
		value:      bestValue,
		source:     best.Source,
		externalID: best.ExternalID,
		strategy:   strategy,
	}, true
}

func selectLongestNonEmpty(records []models.EntityRecord, field, strategy string) (selectedValue, bool) {
	var best *models.EntityRecord
	var bestValue any
	bestLen := -1
	for i := range records {
		value, ok := extractValue(records[i], field)
		if !ok {
			continue
		}
		l := valueLength(value)
		if l > bestLen {
			best = &records[i]
			bestValue = value
			bestLen = l
		}
	}
	if best == nil {
		return selectedValue{}, false
	}
	return selectedValue{
		value:      bestValue,
		source:     best.Source,
		externalID: best.ExternalID,
		strategy:   strategy,
	}, true
}

func selectMostCommon(records []models.EntityRecord, field, strategy string) (selectedValue, bool) {
	type bucket struct {
		count    int
		record   *models.EntityRecord
		value    any
		insertAt int
	}
	counts := map[string]*bucket{}
	insertion := 0
	for i := range records {
		value, ok := extractValue(records[i], field)
		if !ok {
			continue
		}
		var s string
		if str, isString := value.(string); isString {
			s = str
		}
		key := engine.NormalizeText(s)
		if existing, ok := counts[key]; ok {
			existing.count++
			continue
		}
		counts[key] = &bucket{count: 1, record: &records[i], value: value, insertAt: insertion}
		insertion++
	}
	if len(counts) == 0 {
		return selectedValue{}, false
	}
	// Stable max — Rust uses BTreeMap iter order (key-sorted) + max_by
	// (returns the last max). We replicate by iterating sorted keys.
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var winner *bucket
	for _, k := range keys {
		b := counts[k]
		if winner == nil || b.count >= winner.count {
			winner = b
		}
	}
	return selectedValue{
		value:      winner.value,
		source:     winner.record.Source,
		externalID: winner.record.ExternalID,
		strategy:   strategy,
	}, true
}

func extractValue(record models.EntityRecord, field string) (any, bool) {
	switch field {
	case "display_name", "name":
		return record.DisplayName, true
	default:
		v, ok := record.Attributes[field]
		if !ok {
			return nil, false
		}
		if v == nil {
			return nil, false
		}
		if s, isStr := v.(string); isStr && strings.TrimSpace(s) == "" {
			return nil, false
		}
		return v, true
	}
}

func valueLength(value any) int {
	if s, ok := value.(string); ok {
		return len(s)
	}
	return 0
}

func defaultSurvivorshipRules() []models.SurvivorshipRule {
	return []models.SurvivorshipRule{
		{
			Field:          "display_name",
			Strategy:       "longest_non_empty",
			SourcePriority: []string{"crm", "erp", "support"},
			Fallback:       "highest_confidence",
		},
		{
			Field:          "email",
			Strategy:       "source_priority",
			SourcePriority: []string{"crm", "erp", "support"},
			Fallback:       "most_common",
		},
		{
			Field:          "phone",
			Strategy:       "most_common",
			SourcePriority: []string{},
			Fallback:       "longest_non_empty",
		},
		{
			Field:          "company",
			Strategy:       "most_common",
			SourcePriority: []string{},
			Fallback:       "longest_non_empty",
		},
	}
}

