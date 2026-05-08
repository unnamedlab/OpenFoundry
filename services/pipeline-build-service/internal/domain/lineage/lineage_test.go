// Tests for Phase C lineage — marking propagation, BFS direction,
// graph builder filtering, impact analysis, marking-aware filtering.
package lineage

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// ── Marking ladder ──────────────────────────────────────────────────

func TestMarkingRank(t *testing.T) {
	t.Parallel()
	for marking, want := range map[string]uint8{
		"public":       0,
		"":             0,
		"confidential": 1,
		"pii":          2,
		"unknown":      0,
	} {
		if got := MarkingRank(marking); got != want {
			t.Errorf("%q: got %d want %d", marking, got, want)
		}
	}
}

func TestMaxMarkingsReturnsHighestRank(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input []string
		want  string
	}{
		{nil, "public"},
		{[]string{"public", "public"}, "public"},
		{[]string{"public", "confidential"}, "confidential"},
		{[]string{"confidential", "pii", "public"}, "pii"},
	}
	for _, c := range cases {
		if got := MaxMarkings(c.input); got != c.want {
			t.Errorf("%v: got %q want %q", c.input, got, c.want)
		}
	}
}

func TestRequiresAcknowledgement(t *testing.T) {
	t.Parallel()
	if RequiresMarkingAcknowledgement("public") {
		t.Error("public must not require ack")
	}
	if !RequiresMarkingAcknowledgement("confidential") {
		t.Error("confidential must require ack")
	}
}

func TestMarkingFromDatasetTagsExplicitPrefix(t *testing.T) {
	t.Parallel()
	if got := MarkingFromDatasetTags([]string{"marking:pii"}); got != "pii" {
		t.Errorf("got %q want pii", got)
	}
	if got := MarkingFromDatasetTags([]string{"classification:confidential"}); got != "confidential" {
		t.Errorf("got %q want confidential", got)
	}
	if got := MarkingFromDatasetTags([]string{"PII"}); got != "pii" {
		t.Errorf("case-insensitive PII drift, got %q", got)
	}
	if got := MarkingFromDatasetTags(nil); got != "public" {
		t.Errorf("default must be public, got %q", got)
	}
}

// ── Access checks ──────────────────────────────────────────────────

func TestCanAccessMarkingAdminAlwaysPasses(t *testing.T) {
	t.Parallel()
	c := &authmw.Claims{Roles: []string{"admin"}}
	for _, m := range []string{"public", "confidential", "pii"} {
		if !CanAccessMarking(c, m) {
			t.Errorf("admin must access %q", m)
		}
	}
}

func TestCanAccessMarkingClearance(t *testing.T) {
	t.Parallel()
	attrs, _ := json.Marshal(map[string]any{"classification_clearance": "confidential"})
	c := &authmw.Claims{Attributes: attrs}
	if !CanAccessMarking(c, "confidential") {
		t.Error("confidential clearance must access confidential")
	}
	if CanAccessMarking(c, "pii") {
		t.Error("confidential clearance must NOT access pii")
	}
}

func TestCanAccessMarkingNilClaims(t *testing.T) {
	t.Parallel()
	if !CanAccessMarking(nil, "public") {
		t.Error("nil claims must access public")
	}
	if CanAccessMarking(nil, "confidential") {
		t.Error("nil claims must NOT access confidential")
	}
}

// ── NodeKind / NodeKey ─────────────────────────────────────────────

func TestParseNodeKind(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"dataset", "pipeline", "workflow"} {
		if _, ok := ParseNodeKind(kind); !ok {
			t.Errorf("expected %s to parse", kind)
		}
	}
	if _, ok := ParseNodeKind("nope"); ok {
		t.Error("unknown kind must not parse")
	}
}

// ── BFS / connectivity ─────────────────────────────────────────────

func makeRel(source, target uuid.UUID, srcKind, tgtKind, marking string) LineageRelationRecord {
	return LineageRelationRecord{
		ID:               uuid.New(),
		SourceID:         source,
		SourceKind:       srcKind,
		TargetID:         target,
		TargetKind:       tgtKind,
		RelationKind:     "derives",
		EffectiveMarking: marking,
		Metadata:         json.RawMessage(`{}`),
	}
}

func TestBFSPathsOutgoingFollowsForwardEdges(t *testing.T) {
	t.Parallel()
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	relations := []LineageRelationRecord{
		makeRel(a, b, "dataset", "dataset", "public"),
		makeRel(b, c, "dataset", "dataset", "public"),
	}
	root := NodeKey{ID: a, Kind: NodeKindDataset}
	paths := BFSPaths(root, relations, DirectionOutgoing)
	if _, ok := paths[NodeKey{ID: c, Kind: NodeKindDataset}]; !ok {
		t.Error("outgoing BFS must reach c via a→b→c")
	}
	if _, ok := paths[root]; !ok || len(paths[root]) != 0 {
		t.Error("root must have empty path")
	}
}

func TestBFSPathsIncomingReversesEdges(t *testing.T) {
	t.Parallel()
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	relations := []LineageRelationRecord{
		makeRel(a, b, "dataset", "dataset", "public"),
		makeRel(b, c, "dataset", "dataset", "public"),
	}
	root := NodeKey{ID: c, Kind: NodeKindDataset}
	paths := BFSPaths(root, relations, DirectionIncoming)
	if _, ok := paths[NodeKey{ID: a, Kind: NodeKindDataset}]; !ok {
		t.Error("incoming BFS must reach a from c")
	}
}

func TestCollectConnectedNodesIsUndirected(t *testing.T) {
	t.Parallel()
	a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	relations := []LineageRelationRecord{
		makeRel(a, b, "dataset", "dataset", "public"),
		makeRel(c, b, "dataset", "dataset", "public"),
	}
	root := NodeKey{ID: a, Kind: NodeKindDataset}
	connected := CollectConnectedNodes(root, relations)
	if len(connected) != 3 {
		t.Errorf("expected 3 connected nodes (a,b,c), got %d", len(connected))
	}
	if _, ok := connected[NodeKey{ID: d, Kind: NodeKindDataset}]; ok {
		t.Error("d is independent, must not be connected")
	}
}

// ── Graph builder ──────────────────────────────────────────────────

func TestBuildGraphFiltersUnknownKinds(t *testing.T) {
	t.Parallel()
	a, b := uuid.New(), uuid.New()
	relations := []LineageRelationRecord{
		makeRel(a, b, "dataset", "garbage_kind", "public"),
		makeRel(a, b, "dataset", "dataset", "public"),
	}
	graph := BuildGraph(map[NodeKey]*LineageNodeRecord{}, relations, nil)
	if len(graph.Edges) != 1 {
		t.Errorf("expected 1 edge after filtering, got %d", len(graph.Edges))
	}
}

func TestBuildGraphSyntheticNodesForUnknownIDs(t *testing.T) {
	t.Parallel()
	a, b := uuid.New(), uuid.New()
	graph := BuildGraph(
		map[NodeKey]*LineageNodeRecord{},
		[]LineageRelationRecord{makeRel(a, b, "dataset", "dataset", "public")},
		nil,
	)
	if len(graph.Nodes) != 2 {
		t.Fatalf("expected 2 synthetic nodes, got %d", len(graph.Nodes))
	}
	for _, n := range graph.Nodes {
		var meta map[string]any
		_ = json.Unmarshal(n.Metadata, &meta)
		if meta["synthetic"] != true {
			t.Errorf("expected synthetic flag, got %v", meta)
		}
	}
}

func TestBuildGraphAllowedAllowList(t *testing.T) {
	t.Parallel()
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	relations := []LineageRelationRecord{
		makeRel(a, b, "dataset", "dataset", "public"),
		makeRel(b, c, "dataset", "dataset", "public"),
	}
	allowed := map[NodeKey]struct{}{
		{ID: a, Kind: NodeKindDataset}: {},
		{ID: b, Kind: NodeKindDataset}: {},
	}
	graph := BuildGraph(map[NodeKey]*LineageNodeRecord{}, relations, allowed)
	if len(graph.Edges) != 1 || graph.Edges[0].Target != b {
		t.Errorf("allow-list must filter b→c edge, got %d edges", len(graph.Edges))
	}
}

// ── Impact analysis ────────────────────────────────────────────────

func TestBuildImpactAnalysisPropagatesMarking(t *testing.T) {
	t.Parallel()
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	nodes := map[NodeKey]*LineageNodeRecord{
		{ID: a, Kind: NodeKindDataset}: {EntityID: a, EntityKind: "dataset", Label: "A", Marking: "public", Metadata: json.RawMessage(`{}`)},
		{ID: b, Kind: NodeKindDataset}: {EntityID: b, EntityKind: "dataset", Label: "B", Marking: "confidential", Metadata: json.RawMessage(`{}`)},
		{ID: c, Kind: NodeKindDataset}: {EntityID: c, EntityKind: "dataset", Label: "C", Marking: "pii", Metadata: json.RawMessage(`{}`)},
	}
	rel1 := makeRel(a, b, "dataset", "dataset", "confidential")
	rel2 := makeRel(b, c, "dataset", "dataset", "pii")
	relations := []LineageRelationRecord{rel1, rel2}
	root := NodeKey{ID: a, Kind: NodeKindDataset}
	impact := BuildImpactAnalysis(root, nodes, relations)
	if impact.PropagatedMarking != "pii" {
		t.Errorf("propagated marking must be pii, got %q", impact.PropagatedMarking)
	}
	if len(impact.Downstream) != 2 {
		t.Errorf("expected 2 downstream items, got %d", len(impact.Downstream))
	}
	// Verify distance ordering.
	if impact.Downstream[0].Distance > impact.Downstream[1].Distance {
		t.Error("downstream items must sort by ascending distance")
	}
}

func TestBuildImpactAnalysisRequiresAck(t *testing.T) {
	t.Parallel()
	a, b := uuid.New(), uuid.New()
	nodes := map[NodeKey]*LineageNodeRecord{
		{ID: a, Kind: NodeKindDataset}: {EntityID: a, EntityKind: "dataset", Label: "A", Marking: "public", Metadata: json.RawMessage(`{}`)},
		{ID: b, Kind: NodeKindDataset}: {EntityID: b, EntityKind: "dataset", Label: "B", Marking: "pii", Metadata: json.RawMessage(`{}`)},
	}
	rel := makeRel(a, b, "dataset", "dataset", "pii")
	impact := BuildImpactAnalysis(
		NodeKey{ID: a, Kind: NodeKindDataset},
		nodes,
		[]LineageRelationRecord{rel},
	)
	if len(impact.Downstream) != 1 || !impact.Downstream[0].RequiresAcknowledgement {
		t.Errorf("downstream pii item must require ack, got %+v", impact.Downstream)
	}
}

// ── Filters ────────────────────────────────────────────────────────

func TestFilterGraphForClaimsDropsAboveClearance(t *testing.T) {
	t.Parallel()
	pub := uuid.New()
	conf := uuid.New()
	pii := uuid.New()
	graph := LineageGraph{
		Nodes: []LineageNode{
			{ID: pub, Marking: "public"},
			{ID: conf, Marking: "confidential"},
			{ID: pii, Marking: "pii"},
		},
		Edges: []LineageGraphEdge{
			{ID: uuid.New(), Source: pub, Target: conf, EffectiveMarking: "confidential"},
			{ID: uuid.New(), Source: conf, Target: pii, EffectiveMarking: "pii"},
		},
	}
	attrs, _ := json.Marshal(map[string]any{"classification_clearance": "confidential"})
	claims := &authmw.Claims{Attributes: attrs}
	filtered := FilterGraphForClaims(graph, claims)
	if len(filtered.Nodes) != 2 {
		t.Errorf("expected 2 nodes after filter, got %d", len(filtered.Nodes))
	}
	for _, n := range filtered.Nodes {
		if n.ID == pii {
			t.Error("pii node must be filtered out")
		}
	}
	if len(filtered.Edges) != 1 {
		t.Errorf("expected 1 edge after filter, got %d", len(filtered.Edges))
	}
}

func TestFilterImpactForClaimsForbiddenWhenRootIsBlocked(t *testing.T) {
	t.Parallel()
	impact := LineageImpactAnalysis{Root: LineageNode{Marking: "pii"}}
	if _, err := FilterImpactForClaims(impact, &authmw.Claims{}); err == nil {
		t.Error("expected forbidden error")
	}
}
