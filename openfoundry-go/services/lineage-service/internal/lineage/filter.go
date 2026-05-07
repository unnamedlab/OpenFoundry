package lineage

import (
	"errors"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
)

// ErrInsufficientClearance mirrors the Rust string returned by
// `filter_impact_for_claims` when the caller can't see the root.
var ErrInsufficientClearance = errors.New("forbidden: insufficient classification clearance")

// FilterGraphForClaims ports `filter_graph_for_claims`. Admins pass
// through unchanged; otherwise we drop nodes whose marking exceeds
// the caller's clearance, and drop any edge that touches a dropped
// node (or whose own effective_marking is too high).
func FilterGraphForClaims(graph models.LineageGraph, claims *authmw.Claims) models.LineageGraph {
	if claims != nil && claims.HasRole("admin") {
		return graph
	}
	allowed := map[uuid.UUID]struct{}{}
	for _, n := range graph.Nodes {
		if CanAccessMarking(claims, n.Marking) {
			allowed[n.ID] = struct{}{}
		}
	}

	nodes := make([]models.LineageNode, 0, len(graph.Nodes))
	for _, n := range graph.Nodes {
		if _, ok := allowed[n.ID]; ok {
			nodes = append(nodes, n)
		}
	}
	edges := make([]models.LineageGraphEdge, 0, len(graph.Edges))
	for _, e := range graph.Edges {
		_, sok := allowed[e.Source]
		_, tok := allowed[e.Target]
		if sok && tok && CanAccessMarking(claims, e.EffectiveMarking) {
			edges = append(edges, e)
		}
	}
	return models.LineageGraph{Nodes: nodes, Edges: edges}
}

// FilterImpactForClaims ports `filter_impact_for_claims`. Returns
// ErrInsufficientClearance when the caller can't see the root.
func FilterImpactForClaims(impact models.LineageImpactAnalysis, claims *authmw.Claims) (models.LineageImpactAnalysis, error) {
	if !CanAccessMarking(claims, impact.Root.Marking) {
		return models.LineageImpactAnalysis{}, ErrInsufficientClearance
	}
	if claims != nil && claims.HasRole("admin") {
		return impact, nil
	}
	upstream := make([]models.LineageImpactItem, 0, len(impact.Upstream))
	for _, item := range impact.Upstream {
		if CanAccessMarking(claims, item.EffectiveMarking) {
			upstream = append(upstream, item)
		}
	}
	downstream := make([]models.LineageImpactItem, 0, len(impact.Downstream))
	for _, item := range impact.Downstream {
		if CanAccessMarking(claims, item.EffectiveMarking) {
			downstream = append(downstream, item)
		}
	}
	allowedIDs := map[uuid.UUID]struct{}{}
	for _, item := range downstream {
		allowedIDs[item.ID] = struct{}{}
	}
	candidates := make([]models.LineageBuildCandidate, 0, len(impact.BuildCandidates))
	for _, c := range impact.BuildCandidates {
		_, ok := allowedIDs[c.ID]
		if ok && CanAccessMarking(claims, c.EffectiveMarking) {
			candidates = append(candidates, c)
		}
	}
	return models.LineageImpactAnalysis{
		Root:              impact.Root,
		PropagatedMarking: impact.PropagatedMarking,
		Upstream:          upstream,
		Downstream:        downstream,
		BuildCandidates:   candidates,
	}, nil
}
