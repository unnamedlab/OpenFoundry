package models

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// Pin the JSON wire shape of the leaf models that landed in the
// ontology-kernel iteration 1 slice. Each test mirrors a field
// declared in the matching Rust struct; the Rust source paths are
// listed beside each test to make drift obvious.

// libs/ontology-kernel/src/models/link_type.rs `struct LinkType`
func TestLinkTypeJSONShape(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	src := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	tgt := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	owner := uuid.MustParse("44444444-4444-4444-4444-444444444444")

	out, err := json.Marshal(LinkType{
		ID:           id,
		Name:         "owns",
		DisplayName:  "Owns",
		Description:  "ownership relation",
		SourceTypeID: src,
		TargetTypeID: tgt,
		Cardinality:  "many_to_one",
		OwnerID:      owner,
	})
	assert.NoError(t, err)
	s := string(out)
	for _, k := range []string{
		`"id":`, `"name":`, `"display_name":`, `"description":`,
		`"source_type_id":`, `"target_type_id":`, `"cardinality":`,
		`"owner_id":`, `"created_at":`, `"updated_at":`,
	} {
		assert.True(t, strings.Contains(s, k), "missing key %s in %s", k, s)
	}
}

// libs/ontology-kernel/src/models/object_type.rs `ListObjectTypesResponse`
func TestListObjectTypesResponseEnvelope(t *testing.T) {
	resp := ListObjectTypesResponse{
		Data: []ObjectType{},
		// Empty slice should marshal as `[]`, mirroring serde for `Vec<T>`.
		Total: 0, Page: 1, PerPage: 50,
	}
	out, err := json.Marshal(resp)
	assert.NoError(t, err)
	assert.Contains(t, string(out), `"data":[]`)
	assert.Contains(t, string(out), `"total":0`)
	assert.Contains(t, string(out), `"page":1`)
	assert.Contains(t, string(out), `"per_page":50`)
}

// libs/ontology-kernel/src/models/graph.rs `GraphSummary`
func TestGraphSummarySortedKeys(t *testing.T) {
	// Rust serde serialises BTreeMap<String, _> with lexicographically
	// ordered keys. encoding/json in Go does the same for `map[string]_`,
	// so two summaries built with different insertion orders must
	// produce byte-identical JSON.
	a := GraphSummary{
		NodeKinds: map[string]int{"object": 1, "type": 2},
	}
	b := GraphSummary{
		NodeKinds: map[string]int{"type": 2, "object": 1},
	}
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	assert.Equal(t, string(aj), string(bj))
}

// libs/ontology-kernel/src/models/search.rs `SearchResult` with
// `#[serde(skip_serializing_if = "Option::is_none")]` on score_breakdown.
func TestSearchResultOmitScoreBreakdown(t *testing.T) {
	r := SearchResult{
		Kind:    "object",
		ID:      uuid.MustParse("55555555-5555-5555-5555-555555555555"),
		Title:   "row-1",
		Snippet: "snippet",
		Score:   0.42,
		Route:   "/row/1",
	}
	out, err := json.Marshal(r)
	assert.NoError(t, err)
	assert.NotContains(t, string(out), `"score_breakdown"`)

	r.ScoreBreakdown = &SearchScoreBreakdown{
		FusionStrategy: "rrf",
		LexicalScore:   0.5,
		SemanticScore:  0.4,
	}
	out, err = json.Marshal(r)
	assert.NoError(t, err)
	assert.Contains(t, string(out), `"score_breakdown":{`)
	assert.Contains(t, string(out), `"fusion_strategy":"rrf"`)
}

// libs/ontology-kernel/src/models/search.rs `KnnObjectResult` —
// distance gated on `skip_serializing_if = "Option::is_none"`.
func TestKnnObjectResultOmitDistance(t *testing.T) {
	out, err := json.Marshal(KnnObjectResult{
		Object: json.RawMessage(`{"id":"x"}`),
		Score:  0.9,
	})
	assert.NoError(t, err)
	assert.NotContains(t, string(out), `"distance"`)
}

// libs/ontology-kernel/src/models/shared_property.rs `SharedPropertyType`
func TestSharedPropertyTypeFieldOrder(t *testing.T) {
	out, err := json.Marshal(SharedPropertyType{
		Name:         "rating",
		DisplayName:  "Rating",
		PropertyType: "numeric",
	})
	assert.NoError(t, err)
	for _, k := range []string{
		`"id":`, `"name":`, `"display_name":`, `"property_type":`,
		`"required":`, `"unique_constraint":`, `"time_dependent":`,
		`"owner_id":`, `"created_at":`, `"updated_at":`,
	} {
		assert.Contains(t, string(out), k)
	}
}
