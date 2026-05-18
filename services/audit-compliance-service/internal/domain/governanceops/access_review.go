package governanceops

import (
	"strings"
	"time"
)

type ReviewEntitlementType string

const (
	EntitlementProjectRole       ReviewEntitlementType = "project_role"
	EntitlementMarkingMembership ReviewEntitlementType = "marking_membership"
	EntitlementGroupMembership   ReviewEntitlementType = "group_membership"
	EntitlementThirdPartyApp     ReviewEntitlementType = "third_party_app_enablement"
	EntitlementServiceUser       ReviewEntitlementType = "service_user"
	EntitlementEgressImporter    ReviewEntitlementType = "egress_importer"
	EntitlementAdminPermission   ReviewEntitlementType = "admin_permission"
)

type AccessReviewItem struct {
	ID              string                `json:"id"`
	Type            ReviewEntitlementType `json:"type"`
	PrincipalID     string                `json:"principal_id"`
	ResourceID      string                `json:"resource_id"`
	Role            string                `json:"role,omitempty"`
	Decision        string                `json:"decision,omitempty"`
	Reviewer        string                `json:"reviewer,omitempty"`
	ExceptionReason string                `json:"exception_reason,omitempty"`
}
type AccessReview struct {
	ID                   string               `json:"id"`
	Name                 string               `json:"name"`
	ScheduledAt          time.Time            `json:"scheduled_at"`
	DueAt                time.Time            `json:"due_at"`
	Status               string               `json:"status"`
	Items                []AccessReviewItem   `json:"items"`
	EffectiveAccessGraph AccessGraph          `json:"effective_access_graph"`
	GroupProjectImpact   []GroupProjectImpact `json:"group_project_impact"`
	BlockingReasons      []string             `json:"blocking_reasons"`
}
type AccessGraph struct {
	Nodes []AccessGraphNode `json:"nodes"`
	Edges []AccessGraphEdge `json:"edges"`
}
type AccessGraphNode struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
}
type AccessGraphEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
}
type GroupProjectImpact struct {
	GroupID              string   `json:"group_id"`
	ProjectID            string   `json:"project_id"`
	AffectedPrincipalIDs []string `json:"affected_principal_ids"`
	EffectiveRole        string   `json:"effective_role"`
}

func ScheduleAccessReview(name string, cadenceDays int, now time.Time, items []AccessReviewItem) AccessReview {
	if cadenceDays <= 0 {
		cadenceDays = 90
	}
	now = nonzero(now)
	review := AccessReview{ID: "review-" + now.Format("20060102150405"), Name: strings.TrimSpace(name), ScheduledAt: now, DueAt: now.Add(time.Duration(cadenceDays) * 24 * time.Hour), Status: "open", Items: append([]AccessReviewItem(nil), items...)}
	if review.Name == "" {
		review.Name = "Access recertification"
	}
	review.EffectiveAccessGraph = BuildAccessGraph(review.Items)
	review.GroupProjectImpact = BuildGroupProjectImpact(review.Items)
	return review
}

func BuildAccessGraph(items []AccessReviewItem) AccessGraph {
	nodes := map[string]AccessGraphNode{}
	edges := []AccessGraphEdge{}
	for _, item := range items {
		p := strings.TrimSpace(item.PrincipalID)
		r := strings.TrimSpace(item.ResourceID)
		if p == "" || r == "" {
			continue
		}
		nodes[p] = AccessGraphNode{ID: p, Kind: "principal", Label: p}
		nodes[r] = AccessGraphNode{ID: r, Kind: "resource", Label: r}
		relation := string(item.Type)
		if item.Role != "" {
			relation += ":" + item.Role
		}
		edges = append(edges, AccessGraphEdge{From: p, To: r, Relation: relation})
	}
	out := AccessGraph{Edges: edges}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, n)
	}
	return out
}

func BuildGroupProjectImpact(items []AccessReviewItem) []GroupProjectImpact {
	impacts := []GroupProjectImpact{}
	for _, item := range items {
		if item.Type == EntitlementProjectRole && strings.HasPrefix(item.PrincipalID, "group:") {
			impacts = append(impacts, GroupProjectImpact{GroupID: strings.TrimPrefix(item.PrincipalID, "group:"), ProjectID: item.ResourceID, EffectiveRole: item.Role})
		}
	}
	return impacts
}

func CloseAccessReview(review AccessReview) AccessReview {
	review.BlockingReasons = nil
	for _, item := range review.Items {
		switch strings.ToLower(strings.TrimSpace(item.Decision)) {
		case "attested", "remove", "exception":
			if item.Decision == "exception" && strings.TrimSpace(item.ExceptionReason) == "" {
				review.BlockingReasons = append(review.BlockingReasons, item.ID+" exception requires reason")
			}
		default:
			review.BlockingReasons = append(review.BlockingReasons, item.ID+" requires attestation, removal, or exception")
		}
	}
	if len(review.BlockingReasons) == 0 {
		review.Status = "closed"
	}
	return review
}
