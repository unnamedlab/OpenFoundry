package domain

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

func toRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func strPtr(s string) *string { return &s }

// libs/ontology-kernel/src/domain/graph.rs
// `object_graph_summary_captures_sensitive_cross_boundary_scope`.
func TestObjectGraphSummaryCapturesSensitiveCrossBoundaryScope(t *testing.T) {
	nodes := []models.GraphNode{
		{
			ID:             "object:root",
			Kind:           "object_instance",
			Label:          "Root",
			SecondaryLabel: strPtr("Case"),
			Metadata: toRaw(t, map[string]any{
				"distance_from_root": 0,
				"organization_id":    "org-a",
				"marking":            "public",
			}),
		},
		{
			ID:             "object:neighbor",
			Kind:           "object_instance",
			Label:          "Neighbor",
			SecondaryLabel: strPtr("Customer"),
			Metadata: toRaw(t, map[string]any{
				"distance_from_root": 1,
				"organization_id":    "org-b",
				"marking":            "pii",
			}),
		},
	}
	edges := []models.GraphEdge{{
		ID:       "link:1",
		Kind:     "link_instance",
		Source:   "object:root",
		Target:   "object:neighbor",
		Label:    "linked",
		Metadata: toRaw(t, map[string]any{}),
	}}

	summary := SummarizeGraph("object", nodes, edges)

	assert.Equal(t, "sensitive_connected", summary.Scope)
	assert.Equal(t, 1, summary.RootNeighborCount)
	assert.Equal(t, 1, summary.MaxHopsReached)
	assert.Equal(t, 1, summary.BoundaryCrossings)
	assert.Equal(t, 1, summary.SensitiveObjects)
	assert.Equal(t, 1, summary.ObjectTypes["Case"])
	assert.Equal(t, 1, summary.ObjectTypes["Customer"])
	assert.Equal(t, 1, summary.Markings["pii"])
}

// libs/ontology-kernel/src/domain/graph.rs
// `schema_graph_summary_stays_in_schema_scope`.
func TestSchemaGraphSummaryStaysInSchemaScope(t *testing.T) {
	nodes := []models.GraphNode{{
		ID:             "type:1",
		Kind:           "object_type",
		Label:          "Case",
		SecondaryLabel: strPtr("case"),
		Metadata:       toRaw(t, map[string]any{}),
	}}

	summary := SummarizeGraph("schema", nodes, nil)

	assert.Equal(t, "schema", summary.Scope)
	assert.Equal(t, 1, summary.ObjectTypes["Case"])
	assert.Equal(t, 0, summary.RootNeighborCount)
	assert.Equal(t, 0, summary.SensitiveObjects)
}

// libs/ontology-kernel/src/domain/graph.rs `classify_scope` —
// the four-branch ladder pinned end-to-end.
func TestClassifyScopeBranches(t *testing.T) {
	cases := []struct {
		mode      string
		neighbors int
		sensitive int
		boundary  int
		want      string
	}{
		{"schema", 5, 5, 5, "schema"},
		{"object", 0, 1, 0, "sensitive_connected"},
		{"object", 0, 0, 1, "cross_boundary"},
		{"object", 1, 0, 0, "connected"},
		{"object", 0, 0, 0, "local"},
	}
	for _, tc := range cases {
		got := classifyScope(tc.mode, tc.neighbors, tc.sensitive, tc.boundary)
		assert.Equal(t, tc.want, got, "mode=%s neighbors=%d sensitive=%d boundary=%d",
			tc.mode, tc.neighbors, tc.sensitive, tc.boundary)
	}
}

// libs/ontology-kernel/src/domain/graph.rs `object_label` — picks
// primary key value when set; falls back to id otherwise.
func TestObjectLabelPrefersPrimaryKey(t *testing.T) {
	id := uuid.New()
	primary := "case_number"
	objType := models.ObjectType{
		ID:                 uuid.New(),
		PrimaryKeyProperty: &primary,
	}
	withPK := &ObjectInstance{
		ID:         id,
		Properties: json.RawMessage(`{"case_number":"C-7","other":"x"}`),
	}
	assert.Equal(t, "C-7", objectLabel(objType, withPK))

	emptyPK := &ObjectInstance{
		ID:         id,
		Properties: json.RawMessage(`{"case_number":""}`),
	}
	assert.Equal(t, id.String(), objectLabel(objType, emptyPK))

	missing := &ObjectInstance{
		ID:         id,
		Properties: json.RawMessage(`{}`),
	}
	assert.Equal(t, id.String(), objectLabel(objType, missing))

	noPK := models.ObjectType{ID: uuid.New(), PrimaryKeyProperty: nil}
	assert.Equal(t, id.String(), objectLabel(noPK, withPK))
}

// libs/ontology-kernel/src/domain/graph.rs `object_label` — non-string
// primary-key values stringify via JSON Marshal (Rust serde_json::to_string).
func TestObjectLabelStringifiesNonStringPrimaryKey(t *testing.T) {
	id := uuid.New()
	primary := "score"
	objType := models.ObjectType{ID: uuid.New(), PrimaryKeyProperty: &primary}
	obj := &ObjectInstance{
		ID:         id,
		Properties: json.RawMessage(`{"score": 99}`),
	}
	got := objectLabel(objType, obj)
	// Rust emits `99` (numeric debug); the Go helper falls back to
	// the raw JSON bytes which is also `99`.
	assert.Equal(t, "99", strings.TrimSpace(got))
}

// libs/ontology-kernel/src/domain/graph.rs node + edge id helpers.
func TestNodeAndEdgeIDFormats(t *testing.T) {
	tid := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	iid := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	oid := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	assert.Equal(t, "type:11111111-1111-1111-1111-111111111111", typeNodeID(tid))
	assert.Equal(t, "interface:22222222-2222-2222-2222-222222222222", interfaceNodeID(iid))
	assert.Equal(t, "object:33333333-3333-3333-3333-333333333333", objectNodeID(oid))
	assert.Equal(t,
		"/ontology/11111111-1111-1111-1111-111111111111#object-33333333-3333-3333-3333-333333333333",
		objectRoute(tid, oid),
	)
}

// libs/ontology-kernel/src/domain/graph.rs SummarizeGraph — sensitive_markings
// is sorted (BTreeSet → Vec in Rust).
func TestSummarizeGraphSensitiveMarkingsSorted(t *testing.T) {
	nodes := []models.GraphNode{
		{
			ID: "object:1", Kind: "object_instance",
			Metadata: toRaw(t, map[string]any{"marking": "secret"}),
		},
		{
			ID: "object:2", Kind: "object_instance",
			Metadata: toRaw(t, map[string]any{"marking": "confidential"}),
		},
		{
			ID: "object:3", Kind: "object_instance",
			Metadata: toRaw(t, map[string]any{"marking": "pii"}),
		},
	}
	summary := SummarizeGraph("object", nodes, nil)
	assert.Equal(t, []string{"confidential", "pii", "secret"}, summary.SensitiveMarkings)
}

// libs/ontology-kernel/src/domain/graph.rs SummarizeGraph — boundary
// crossings count edges whose endpoints carry distinct organization_id.
func TestSummarizeGraphBoundaryCrossings(t *testing.T) {
	nodes := []models.GraphNode{
		{
			ID:       "object:a",
			Kind:     "object_instance",
			Metadata: toRaw(t, map[string]any{"organization_id": "org-1"}),
		},
		{
			ID:       "object:b",
			Kind:     "object_instance",
			Metadata: toRaw(t, map[string]any{"organization_id": "org-2"}),
		},
		{
			ID:       "object:c",
			Kind:     "object_instance",
			Metadata: toRaw(t, map[string]any{}),
		},
	}
	edges := []models.GraphEdge{
		{ID: "link:1", Kind: "link_instance", Source: "object:a", Target: "object:b"},
		{ID: "link:2", Kind: "link_instance", Source: "object:a", Target: "object:c"}, // org → none
		{ID: "link:3", Kind: "link_instance", Source: "object:c", Target: "object:c"}, // none → none, no crossing
	}
	summary := SummarizeGraph("object", nodes, edges)
	assert.Equal(t, 2, summary.BoundaryCrossings)
}
