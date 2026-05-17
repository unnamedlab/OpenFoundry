package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/repo"
)

// EvaluationResult mirrors the Rust EvaluationResult shape byte-for-byte.
// Carried by POST /api/v1/policy-evaluations as the response body.
type EvaluationResult struct {
	Allowed                    bool                        `json:"allowed"`
	MatchedPolicyIDs           []uuid.UUID                 `json:"matched_policy_ids"`
	DenyPolicyIDs              []uuid.UUID                 `json:"deny_policy_ids"`
	RowFilter                  *string                     `json:"row_filter"`
	HiddenColumns              []string                    `json:"hidden_columns"`
	MatchedRestrictedViewIDs   []uuid.UUID                 `json:"matched_restricted_view_ids"`
	RestrictedViews            []EvaluationRestrictedView  `json:"restricted_views"`
	DenyReasons                []string                    `json:"deny_reasons"`
	AllowedOrgIDs              []uuid.UUID                 `json:"allowed_org_ids"`
	AllowedMarkings            []string                    `json:"allowed_markings"`
	EffectiveClearance         *string                     `json:"effective_clearance"`
	ConsumerMode               bool                        `json:"consumer_mode"`
}

// EvaluationRestrictedView is the per-view row attached to an evaluation.
type EvaluationRestrictedView struct {
	ID                  uuid.UUID   `json:"id"`
	Name                string      `json:"name"`
	RowFilter           *string     `json:"row_filter"`
	HiddenColumns       []string    `json:"hidden_columns"`
	AllowedOrgIDs       []uuid.UUID `json:"allowed_org_ids"`
	AllowedMarkings     []string    `json:"allowed_markings"`
	ConsumerModeEnabled bool        `json:"consumer_mode_enabled"`
	AllowGuestAccess    bool        `json:"allow_guest_access"`
}

// Evaluate runs the ABAC evaluator over the configured abac_policies +
// restricted_views for (resource, action), against the supplied caller
// claims and resource attributes.
//
// Mirrors libs/authz-cedar's NOT used here — this is the legacy ABAC
// surface that pre-dates Cedar. The two evaluators coexist: services
// gate write paths via Cedar, ABAC is read-time row filtering + view
// scoping. Decision matrix matches the Rust impl byte-for-byte:
//
//   1. Org isolation + classification boundary checks → deny_reasons.
//   2. For each policy whose conditions match: allow → matched, with
//      optional row_filter; deny → deny_policy_ids.
//   3. For each restricted view: scope checks (org/markings/guest/
//      scoped IDs), accumulate row_filter + hidden_columns +
//      consumer_mode_enabled.
//   4. Final allowed = no denies AND (admin OR no controls OR something
//      matched).
func Evaluate(
	ctx context.Context,
	r *repo.Repo,
	claims *authmw.Claims,
	tenantID uuid.UUID,
	resource, action string,
	resourceAttributes json.RawMessage,
) (*EvaluationResult, error) {
	policies, err := r.ListEnabledABACPoliciesMatching(ctx, tenantID, resource, action)
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}
	views, err := r.ListEnabledRestrictedViewsMatching(ctx, resource, action)
	if err != nil {
		return nil, fmt.Errorf("list restricted views: %w", err)
	}

	hasConfiguredPolicies := len(policies) > 0
	hasConfiguredRestrictedViews := len(views) > 0

	subjectCtx := buildSubjectContext(claims)
	resourceAttrs := decodeAttrs(resourceAttributes)
	resourceOrg := resourceOrgID(resourceAttrs)
	resourceMark := resourceMarking(resourceAttrs)
	scopedViewIDs := claims.RestrictedViewIDs()

	var (
		matched         []uuid.UUID
		denyIDs         []uuid.UUID
		allowFilters    []string
		restrictedFilts []string
		hiddenCols      []string
		matchedViewIDs  []uuid.UUID
		evalViews       []EvaluationRestrictedView
		denyReasons     []string
		consumerMode    = claims.ConsumerModeEnabled()
	)

	if !claims.HasRole("admin") && !claims.AllowsOrgID(resourceOrg) {
		denyReasons = append(denyReasons, "organization isolation boundary denied access")
	}
	if resourceMark != nil {
		if !claims.HasRole("admin") && !claims.AllowsMarking(*resourceMark) {
			denyReasons = append(denyReasons,
				fmt.Sprintf("classification boundary denied marking '%s'", *resourceMark))
		}
	}

	for _, p := range policies {
		if !policyMatches(p.Conditions, subjectCtx, resourceAttrs) {
			continue
		}
		if strings.EqualFold(p.Effect, "deny") {
			denyIDs = append(denyIDs, p.ID)
			continue
		}
		matched = append(matched, p.ID)
		if p.RowFilter != nil && *p.RowFilter != "" {
			rendered := renderRowFilter(*p.RowFilter, subjectCtx, resourceAttrs)
			if rendered != "" {
				allowFilters = append(allowFilters, rendered)
			}
		}
	}

	for _, v := range views {
		if !policyMatches(v.Conditions, subjectCtx, resourceAttrs) {
			continue
		}
		if !claims.HasRole("admin") && len(scopedViewIDs) > 0 {
			if !containsUUID(scopedViewIDs, v.ID) {
				continue
			}
		}
		if claims.IsGuestSession() && !v.AllowGuestAccess {
			continue
		}
		if len(v.AllowedOrgIDs) > 0 {
			if resourceOrg == nil {
				continue
			}
			if !containsUUID(v.AllowedOrgIDs, *resourceOrg) {
				continue
			}
		}
		if len(v.AllowedMarkings) > 0 {
			mark := "public"
			if resourceMark != nil {
				mark = *resourceMark
			}
			ok := false
			for _, c := range v.AllowedMarkings {
				if strings.EqualFold(c, mark) {
					ok = true
					break
				}
			}
			if !ok {
				continue
			}
		}
		matchedViewIDs = append(matchedViewIDs, v.ID)
		if v.RowFilter != nil && *v.RowFilter != "" {
			rendered := renderRowFilter(*v.RowFilter, subjectCtx, resourceAttrs)
			if rendered != "" {
				restrictedFilts = append(restrictedFilts, rendered)
			}
		}
		hiddenCols = append(hiddenCols, v.HiddenColumns...)
		consumerMode = consumerMode || v.ConsumerModeEnabled
		evalViews = append(evalViews, EvaluationRestrictedView{
			ID: v.ID, Name: v.Name, RowFilter: v.RowFilter,
			HiddenColumns:   append([]string(nil), v.HiddenColumns...),
			AllowedOrgIDs:   append([]uuid.UUID(nil), v.AllowedOrgIDs...),
			AllowedMarkings: append([]string(nil), v.AllowedMarkings...),
			ConsumerModeEnabled: v.ConsumerModeEnabled,
			AllowGuestAccess:    v.AllowGuestAccess,
		})
	}

	sort.Strings(hiddenCols)
	hiddenCols = dedupSorted(hiddenCols)

	allowSQL := joinWithOr(allowFilters)
	restrSQL := joinWithOr(restrictedFilts)
	finalRowFilter := joinWithAnd(allowSQL, restrSQL)

	hasAccessControls := hasConfiguredPolicies || hasConfiguredRestrictedViews ||
		len(denyIDs) > 0 || len(denyReasons) > 0
	allowed := len(denyIDs) == 0 && len(denyReasons) == 0 &&
		(claims.HasRole("admin") || !hasAccessControls ||
			len(matched) > 0 || len(matchedViewIDs) > 0)

	clearance, hasClearance := claims.ClassificationClearance()
	var clearancePtr *string
	if hasClearance {
		clearancePtr = &clearance
	}

	return &EvaluationResult{
		Allowed:                  allowed,
		MatchedPolicyIDs:         emptyUUIDSlice(matched),
		DenyPolicyIDs:            emptyUUIDSlice(denyIDs),
		RowFilter:                finalRowFilter,
		HiddenColumns:            emptyStringSlice(hiddenCols),
		MatchedRestrictedViewIDs: emptyUUIDSlice(matchedViewIDs),
		RestrictedViews:          emptyViewSlice(evalViews),
		DenyReasons:              emptyStringSlice(denyReasons),
		AllowedOrgIDs:            emptyUUIDSlice(claims.AllowedOrgIDs()),
		AllowedMarkings:          emptyStringSlice(claims.AllowedMarkings()),
		EffectiveClearance:       clearancePtr,
		ConsumerMode:             consumerMode,
	}, nil
}

// ─── helpers ────────────────────────────────────────────────────────

type ctxMap = map[string]any

func buildSubjectContext(claims *authmw.Claims) ctxMap {
	out := ctxMap{
		"user_id":             claims.Sub.String(),
		"organization_id":     nil,
		"roles":               claims.Roles,
		"permissions":         claims.Permissions,
		"allowed_org_ids":     claims.AllowedOrgIDs(),
		"allowed_markings":    claims.AllowedMarkings(),
		"restricted_view_ids": claims.RestrictedViewIDs(),
		"consumer_mode":       claims.ConsumerModeEnabled(),
	}
	if claims.OrgID != nil {
		out["organization_id"] = claims.OrgID.String()
	}
	if len(claims.Attributes) > 0 {
		var attrs map[string]any
		if err := json.Unmarshal(claims.Attributes, &attrs); err == nil {
			for k, v := range attrs {
				out[k] = v
			}
		}
	}
	return out
}

func decodeAttrs(raw json.RawMessage) ctxMap {
	if len(raw) == 0 {
		return ctxMap{}
	}
	var out ctxMap
	if err := json.Unmarshal(raw, &out); err != nil {
		return ctxMap{}
	}
	return out
}

func policyMatches(conditions json.RawMessage, subject, resource ctxMap) bool {
	if len(conditions) == 0 {
		return true
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(conditions, &root); err != nil {
		return true
	}
	return matchSelector(root["subject"], subject, resource) &&
		matchSelector(root["resource"], resource, subject)
}

func matchSelector(selector json.RawMessage, context, otherContext ctxMap) bool {
	if len(selector) == 0 {
		return true
	}
	var sel map[string]any
	if err := json.Unmarshal(selector, &sel); err != nil {
		return true
	}
	for key, expected := range sel {
		actual, ok := context[key]
		if !ok {
			actual = nil
		}
		if !valueMatches(actual, expected, otherContext) {
			return false
		}
	}
	return true
}

func valueMatches(actual, expected any, otherContext ctxMap) bool {
	switch e := expected.(type) {
	case string:
		if strings.HasPrefix(e, "$other.") {
			key := strings.TrimPrefix(e, "$other.")
			return jsonEqual(actual, otherContext[key])
		}
		return jsonEqual(actual, e)
	case []any:
		if a, ok := actual.([]any); ok {
			for _, item := range a {
				for _, c := range e {
					if jsonEqual(item, c) {
						return true
					}
				}
			}
			return false
		}
		for _, c := range e {
			if jsonEqual(actual, c) {
				return true
			}
		}
		return false
	default:
		return jsonEqual(actual, expected)
	}
}

func jsonEqual(a, b any) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ab) == string(bb)
}

func renderRowFilter(template string, subject, resource ctxMap) string {
	out := replaceTokens(template, "subject", subject)
	return replaceTokens(out, "resource", resource)
}

func replaceTokens(input, prefix string, ctx ctxMap) string {
	for k, v := range ctx {
		token := "{{" + prefix + "." + k + "}}"
		var replacement string
		switch x := v.(type) {
		case nil:
			replacement = "NULL"
		case string:
			replacement = x
		default:
			b, err := json.Marshal(x)
			if err != nil {
				continue
			}
			replacement = string(b)
		}
		input = strings.ReplaceAll(input, token, replacement)
	}
	return input
}

func joinWithOr(filters []string) string {
	if len(filters) == 0 {
		return ""
	}
	parts := make([]string, 0, len(filters))
	for _, f := range filters {
		parts = append(parts, "("+f+")")
	}
	return strings.Join(parts, " OR ")
}

func joinWithAnd(filters ...string) *string {
	parts := make([]string, 0, len(filters))
	for _, f := range filters {
		f = strings.TrimSpace(f)
		if f != "" {
			parts = append(parts, f)
		}
	}
	switch len(parts) {
	case 0:
		return nil
	case 1:
		return &parts[0]
	default:
		wrapped := make([]string, 0, len(parts))
		for _, p := range parts {
			wrapped = append(wrapped, "("+p+")")
		}
		out := strings.Join(wrapped, " AND ")
		return &out
	}
}

func resourceOrgID(attrs ctxMap) *uuid.UUID {
	for _, k := range []string{"organization_id", "org_id"} {
		if v, ok := attrs[k]; ok {
			if s, ok := v.(string); ok {
				if id, err := uuid.Parse(s); err == nil {
					return &id
				}
			}
		}
	}
	return nil
}

func resourceMarking(attrs ctxMap) *string {
	for _, k := range []string{"effective_marking", "marking"} {
		if v, ok := attrs[k]; ok {
			if s, ok := v.(string); ok {
				lower := strings.ToLower(s)
				if MarkingRank(lower) >= 0 {
					return &lower
				}
			}
		}
	}
	return nil
}

func containsUUID(set []uuid.UUID, target uuid.UUID) bool {
	for _, x := range set {
		if x == target {
			return true
		}
	}
	return false
}

func emptyUUIDSlice(s []uuid.UUID) []uuid.UUID {
	if s == nil {
		return []uuid.UUID{}
	}
	return s
}

func emptyStringSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func emptyViewSlice(s []EvaluationRestrictedView) []EvaluationRestrictedView {
	if s == nil {
		return []EvaluationRestrictedView{}
	}
	return s
}
