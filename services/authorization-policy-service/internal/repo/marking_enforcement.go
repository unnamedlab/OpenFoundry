// marking_enforcement.go: SG.14 effective marking resolution,
// inheritance provenance, and resource/data access checks.

package repo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

const defaultResourceMarkingInheritanceDepth = 8

type resourceMarkingPath struct {
	ResourceKind  string
	ResourceID    string
	RelationKinds []string
	PathTokens    []string
}

type directResourceMarking struct {
	Item        *models.ResourceMarking
	MarkingName string
}

type resourceMarkingQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (r *Repo) ListResourceMarkingEdges(ctx context.Context, tenantID *uuid.UUID, resourceKind, resourceID, direction string) ([]models.ResourceMarkingEdge, error) {
	pred, args := tenantPredicate("resource_marking_edges", tenantID, 1)
	conds := []string{pred}
	if resourceKind != "" && resourceID != "" {
		switch direction {
		case "upstream":
			args = append(args, resourceKind, resourceID)
			conds = append(conds, fmt.Sprintf("target_resource_kind = $%d AND target_resource_id = $%d", len(args)-1, len(args)))
		case "downstream":
			args = append(args, resourceKind, resourceID)
			conds = append(conds, fmt.Sprintf("source_resource_kind = $%d AND source_resource_id = $%d", len(args)-1, len(args)))
		default:
			args = append(args, resourceKind, resourceID, resourceKind, resourceID)
			conds = append(conds, fmt.Sprintf(
				"((target_resource_kind = $%d AND target_resource_id = $%d) OR (source_resource_kind = $%d AND source_resource_id = $%d))",
				len(args)-3, len(args)-2, len(args)-1, len(args),
			))
		}
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT id, tenant_id, source_resource_kind, source_resource_id,
		        target_resource_kind, target_resource_id, relation_kind,
		        metadata, created_by, created_at, updated_at
		   FROM resource_marking_edges
		  WHERE `+strings.Join(conds, " AND ")+`
		  ORDER BY created_at DESC
		  LIMIT 500`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ResourceMarkingEdge, 0)
	for rows.Next() {
		edge, err := scanResourceMarkingEdge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *edge)
	}
	return out, rows.Err()
}

func (r *Repo) UpsertResourceMarkingEdge(ctx context.Context, tenantID *uuid.UUID, actorID uuid.UUID, body *models.UpsertResourceMarkingEdgeRequest) (*models.ResourceMarkingEdge, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	edge, err := getResourceMarkingEdgeForUpdateTx(ctx, tx, tenantID, body.SourceResourceKind, body.SourceResourceID, body.TargetResourceKind, body.TargetResourceID, body.RelationKind)
	if errors.Is(err, pgx.ErrNoRows) {
		row := tx.QueryRow(ctx,
			`INSERT INTO resource_marking_edges
			    (id, tenant_id, source_resource_kind, source_resource_id,
			     target_resource_kind, target_resource_id, relation_kind,
			     metadata, created_by, created_at, updated_at)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW(),NOW())
			 RETURNING id, tenant_id, source_resource_kind, source_resource_id,
			           target_resource_kind, target_resource_id, relation_kind,
			           metadata, created_by, created_at, updated_at`,
			uuid.New(), tenantID, body.SourceResourceKind, body.SourceResourceID,
			body.TargetResourceKind, body.TargetResourceID, body.RelationKind,
			normalizeJSONObject(body.Metadata), actorID,
		)
		edge, err = scanResourceMarkingEdge(row)
	} else if err == nil {
		row := tx.QueryRow(ctx,
			`UPDATE resource_marking_edges
			    SET metadata = $2, created_by = $3, updated_at = NOW()
			  WHERE id = $1
			  RETURNING id, tenant_id, source_resource_kind, source_resource_id,
			            target_resource_kind, target_resource_id, relation_kind,
			            metadata, created_by, created_at, updated_at`,
			edge.ID, normalizeJSONObject(body.Metadata), actorID,
		)
		edge, err = scanResourceMarkingEdge(row)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return edge, nil
}

func (r *Repo) DeleteResourceMarkingEdge(ctx context.Context, tenantID *uuid.UUID, body *models.DeleteResourceMarkingEdgeRequest) (bool, error) {
	pred, args := tenantPredicate("resource_marking_edges", tenantID, 1)
	args = append(args, body.SourceResourceKind, body.SourceResourceID, body.TargetResourceKind, body.TargetResourceID, body.RelationKind)
	tag, err := r.Pool.Exec(ctx,
		`DELETE FROM resource_marking_edges
		  WHERE `+pred+fmt.Sprintf(`
		    AND source_resource_kind = $%d AND source_resource_id = $%d
		    AND target_resource_kind = $%d AND target_resource_id = $%d
		    AND relation_kind = $%d`, len(args)-4, len(args)-3, len(args)-2, len(args)-1, len(args)),
		args...,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) EffectiveResourceMarkings(ctx context.Context, tenantID *uuid.UUID, resourceKind, resourceID string, maxDepth int) (*models.EffectiveResourceMarkingsResponse, error) {
	return r.effectiveResourceMarkings(ctx, r.Pool, tenantID, resourceKind, resourceID, maxDepth)
}

func (r *Repo) effectiveResourceMarkings(ctx context.Context, q resourceMarkingQueryer, tenantID *uuid.UUID, resourceKind, resourceID string, maxDepth int) (*models.EffectiveResourceMarkingsResponse, error) {
	maxDepth = clampResourceMarkingDepth(maxDepth)
	paths := []resourceMarkingPath{{
		ResourceKind: resourceKind,
		ResourceID:   resourceID,
		PathTokens:   []string{resourceToken(resourceKind, resourceID)},
	}}
	walked, err := r.resourceMarkingInheritancePaths(ctx, q, tenantID, resourceKind, resourceID, maxDepth)
	if err != nil {
		return nil, err
	}
	paths = append(paths, walked...)

	byMarking := map[uuid.UUID]*models.EffectiveResourceMarking{}
	seenSource := map[string]bool{}
	for _, path := range paths {
		rows, err := r.directResourceMarkingsFor(ctx, q, tenantID, path.ResourceKind, path.ResourceID)
		if err != nil {
			return nil, err
		}
		sourceKind := effectiveSourceKind(path.RelationKinds)
		requiredFor := effectiveRequiredFor(path.RelationKinds)
		hops := pathHops(path.PathTokens, path.RelationKinds)
		for _, row := range rows {
			key := row.Item.MarkingID
			sourceKey := key.String() + "|" + row.Item.ID.String() + "|" + strings.Join(path.PathTokens, ">") + "|" + strings.Join(path.RelationKinds, ">")
			if seenSource[sourceKey] {
				continue
			}
			seenSource[sourceKey] = true
			eff := byMarking[key]
			if eff == nil {
				eff = &models.EffectiveResourceMarking{
					MarkingID:   key,
					MarkingName: row.MarkingName,
					RequiredFor: []string{},
					Sources:     []models.EffectiveResourceMarkingSource{},
				}
				byMarking[key] = eff
			}
			eff.RequiredFor = appendUniqueStringSorted(eff.RequiredFor, requiredFor)
			eff.Sources = append(eff.Sources, models.EffectiveResourceMarkingSource{
				SourceKind:              sourceKind,
				RequiredFor:             requiredFor,
				SourceResourceKind:      row.Item.ResourceKind,
				SourceResourceID:        row.Item.ResourceID,
				DirectResourceMarkingID: row.Item.ID,
				RelationKinds:           append([]string{}, path.RelationKinds...),
				Path:                    hops,
				Metadata:                normalizeJSONObject(row.Item.Metadata),
			})
		}
	}

	items := make([]models.EffectiveResourceMarking, 0, len(byMarking))
	for _, item := range byMarking {
		sort.SliceStable(item.Sources, func(i, j int) bool {
			if item.Sources[i].RequiredFor != item.Sources[j].RequiredFor {
				return item.Sources[i].RequiredFor < item.Sources[j].RequiredFor
			}
			if item.Sources[i].SourceKind != item.Sources[j].SourceKind {
				return item.Sources[i].SourceKind < item.Sources[j].SourceKind
			}
			return item.Sources[i].SourceResourceID < item.Sources[j].SourceResourceID
		})
		items = append(items, *item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].MarkingName != items[j].MarkingName {
			return items[i].MarkingName < items[j].MarkingName
		}
		return items[i].MarkingID.String() < items[j].MarkingID.String()
	})

	return &models.EffectiveResourceMarkingsResponse{
		ResourceKind: resourceKind,
		ResourceID:   resourceID,
		Items:        items,
		CheckedAt:    time.Now().UTC(),
	}, nil
}

func (r *Repo) CheckResourceAccess(ctx context.Context, tenantID *uuid.UUID, actorID uuid.UUID, body *models.ResourceAccessCheckRequest, activeAllowedMarkings []string, scopedSessionActive bool) (*models.ResourceAccessCheckResponse, error) {
	principalID := actorID
	if body.PrincipalID != nil {
		principalID = *body.PrincipalID
	}
	maxDepth := 0
	if body.MaxDepth != nil {
		maxDepth = *body.MaxDepth
	}
	effective, err := r.EffectiveResourceMarkings(ctx, tenantID, body.ResourceKind, body.ResourceID, maxDepth)
	if err != nil {
		return nil, err
	}
	markingResults := make([]models.ResourceAccessMarkingResult, 0, len(effective.Items))
	for _, item := range effective.Items {
		set, err := r.markingPermissionSet(ctx, item.MarkingID, principalID, body.GroupIDs)
		if err != nil {
			return nil, err
		}
		member := set[models.MarkingPermissionMember]
		inScope := resourceMarkingAllowedByScope(item, activeAllowedMarkings, scopedSessionActive)
		satisfied := member && inScope
		missingFor := []string{}
		if !satisfied {
			missingFor = append(missingFor, item.RequiredFor...)
		}
		markingResults = append(markingResults, models.ResourceAccessMarkingResult{
			MarkingID:                item.MarkingID,
			MarkingName:              item.MarkingName,
			RequiredFor:              append([]string{}, item.RequiredFor...),
			Satisfied:                satisfied,
			MembershipSatisfied:      member,
			ScopedSessionSatisfied:   inScope,
			ScopedSessionRequirement: scopedSessionActive,
			MissingFor:               missingFor,
			Sources:                  item.Sources,
		})
	}

	orgReq := evaluateResourceOrganizationRequirement(body.RequiredOrganizationID, body.UserOrganizationIDs)
	roleReq := evaluateResourceRoleRequirement(body.RoleSatisfied, body.RoleLabel, body.RoleDetail)
	scopedResourceReq := evaluateScopedSessionRequirement("Scoped session", models.ResourceMarkingRequiredForResourceAccess, activeAllowedMarkings, scopedSessionActive, markingResults)
	resourceMarkingReq := evaluateResourceMarkingRequirement(models.ResourceAccessRequirementResourceMarking, "Resource markings", models.ResourceMarkingRequiredForResourceAccess, markingResults)
	scopedDataReq := evaluateScopedSessionRequirement("Scoped session data markings", models.ResourceMarkingRequiredForDataAccess, activeAllowedMarkings, scopedSessionActive, markingResults)
	dataMarkingReq := evaluateResourceMarkingRequirement(models.ResourceAccessRequirementDataMarking, "Lineage-derived data markings", models.ResourceMarkingRequiredForDataAccess, markingResults)

	accessReqs := []models.ResourceAccessRequirementResult{orgReq, roleReq, scopedResourceReq, resourceMarkingReq}
	dataReqs := []models.ResourceAccessRequirementResult{scopedDataReq, dataMarkingReq}
	resourceAllowed := resourceAccessRequirementsSatisfied(accessReqs)
	dataAllowed := resourceAllowed && resourceAccessRequirementsSatisfied(dataReqs)

	return &models.ResourceAccessCheckResponse{
		PrincipalID:                principalID,
		ResourceKind:               body.ResourceKind,
		ResourceID:                 body.ResourceID,
		ResourceAccessAllowed:      resourceAllowed,
		DataAccessAllowed:          dataAllowed,
		AccessRequirements:         accessReqs,
		AdditionalDataRequirements: dataReqs,
		EffectiveMarkings:          effective.Items,
		MarkingResults:             markingResults,
		CheckedAt:                  time.Now().UTC(),
	}, nil
}

func (r *Repo) resourceMarkingInheritancePaths(ctx context.Context, q resourceMarkingQueryer, tenantID *uuid.UUID, resourceKind, resourceID string, maxDepth int) ([]resourceMarkingPath, error) {
	tenantCond, tenantArgs := tenantPredicate("e", tenantID, 4)
	args := []any{resourceKind, resourceID, maxDepth}
	args = append(args, tenantArgs...)
	query := `WITH RECURSIVE walk AS (
	    SELECT e.source_resource_kind,
	           e.source_resource_id,
	           e.relation_kind,
	           1 AS depth,
	           ARRAY[e.source_resource_kind || ':' || e.source_resource_id,
	                 e.target_resource_kind || ':' || e.target_resource_id]::text[] AS path,
	           ARRAY[e.relation_kind]::text[] AS relation_path
	      FROM resource_marking_edges e
	     WHERE e.target_resource_kind = $1
	       AND e.target_resource_id = $2
	       AND ` + tenantCond + `
	    UNION ALL
	    SELECT e.source_resource_kind,
	           e.source_resource_id,
	           e.relation_kind,
	           w.depth + 1,
	           ARRAY[e.source_resource_kind || ':' || e.source_resource_id]::text[] || w.path,
	           ARRAY[e.relation_kind]::text[] || w.relation_path
	      FROM resource_marking_edges e
	      JOIN walk w
	        ON e.target_resource_kind = w.source_resource_kind
	       AND e.target_resource_id = w.source_resource_id
	     WHERE w.depth < $3
	       AND NOT ((e.source_resource_kind || ':' || e.source_resource_id) = ANY(w.path))
	       AND ` + tenantCond + `
	)
	SELECT source_resource_kind, source_resource_id, relation_path, path
	  FROM walk
	 ORDER BY array_length(path, 1), path`
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]resourceMarkingPath, 0)
	for rows.Next() {
		item := resourceMarkingPath{}
		if err := rows.Scan(&item.ResourceKind, &item.ResourceID, &item.RelationKinds, &item.PathTokens); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repo) directResourceMarkingsFor(ctx context.Context, q resourceMarkingQueryer, tenantID *uuid.UUID, resourceKind, resourceID string) ([]directResourceMarking, error) {
	pred, args := tenantPredicate("rm", tenantID, 1)
	args = append(args, resourceKind, resourceID)
	rows, err := q.Query(ctx,
		`SELECT rm.id, rm.tenant_id, rm.resource_kind, rm.resource_id,
		        rm.marking_id, rm.source_kind, rm.metadata, rm.applied_by,
		        rm.applied_at, COALESCE(NULLIF(m.display_name, ''), m.slug, rm.marking_id::text)
		   FROM resource_markings rm
		   LEFT JOIN markings m ON m.id = rm.marking_id
		  WHERE `+pred+fmt.Sprintf(" AND rm.resource_kind = $%d AND rm.resource_id = $%d", len(args)-1, len(args))+`
		  ORDER BY rm.applied_at DESC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]directResourceMarking, 0)
	for rows.Next() {
		row := directResourceMarking{Item: &models.ResourceMarking{}}
		if err := rows.Scan(&row.Item.ID, &row.Item.TenantID, &row.Item.ResourceKind, &row.Item.ResourceID,
			&row.Item.MarkingID, &row.Item.SourceKind, &row.Item.Metadata, &row.Item.AppliedBy,
			&row.Item.AppliedAt, &row.MarkingName); err != nil {
			return nil, err
		}
		row.Item.Metadata = normalizeJSONObject(row.Item.Metadata)
		out = append(out, row)
	}
	return out, rows.Err()
}

func scanResourceMarkingEdge(row rowLikeT) (*models.ResourceMarkingEdge, error) {
	item := &models.ResourceMarkingEdge{}
	if err := row.Scan(&item.ID, &item.TenantID, &item.SourceResourceKind, &item.SourceResourceID,
		&item.TargetResourceKind, &item.TargetResourceID, &item.RelationKind,
		&item.Metadata, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.Metadata = normalizeJSONObject(item.Metadata)
	return item, nil
}

func getResourceMarkingEdgeForUpdateTx(ctx context.Context, tx pgx.Tx, tenantID *uuid.UUID, sourceKind, sourceID, targetKind, targetID, relationKind string) (*models.ResourceMarkingEdge, error) {
	pred, args := tenantPredicate("resource_marking_edges", tenantID, 1)
	args = append(args, sourceKind, sourceID, targetKind, targetID, relationKind)
	row := tx.QueryRow(ctx,
		`SELECT id, tenant_id, source_resource_kind, source_resource_id,
		        target_resource_kind, target_resource_id, relation_kind,
		        metadata, created_by, created_at, updated_at
		   FROM resource_marking_edges
		  WHERE `+pred+fmt.Sprintf(`
		    AND source_resource_kind = $%d AND source_resource_id = $%d
		    AND target_resource_kind = $%d AND target_resource_id = $%d
		    AND relation_kind = $%d
		  FOR UPDATE`, len(args)-4, len(args)-3, len(args)-2, len(args)-1, len(args)),
		args...,
	)
	return scanResourceMarkingEdge(row)
}

func evaluateResourceOrganizationRequirement(required *uuid.UUID, presentIDs []uuid.UUID) models.ResourceAccessRequirementResult {
	if required == nil {
		return notApplicableResourceRequirement(models.ResourceAccessRequirementOrganization, "Organization boundary", "No organization boundary was supplied.")
	}
	requiredValues := []string{required.String()}
	present := uuidRawStrings(presentIDs)
	passed := uuidInSlice(presentIDs, *required)
	status := models.ResourceAccessRequirementStatusFailed
	detail := "Principal is outside the required organization boundary."
	if passed {
		status = models.ResourceAccessRequirementStatusPassed
		detail = "Principal satisfies the organization boundary."
	}
	return models.ResourceAccessRequirementResult{
		Kind:      models.ResourceAccessRequirementOrganization,
		Label:     "Organization boundary",
		Status:    status,
		Satisfied: passed,
		Required:  requiredValues,
		Present:   present,
		Missing:   missingAccessStrings(requiredValues, present),
		Detail:    detail,
	}
}

func evaluateResourceRoleRequirement(roleSatisfied *bool, label, detail string) models.ResourceAccessRequirementResult {
	if roleSatisfied == nil {
		return notApplicableResourceRequirement(models.ResourceAccessRequirementRole, "Role requirement", "No role requirement evidence was supplied.")
	}
	if strings.TrimSpace(label) == "" {
		label = "Role requirement"
	}
	status := models.ResourceAccessRequirementStatusFailed
	if *roleSatisfied {
		status = models.ResourceAccessRequirementStatusPassed
	}
	if strings.TrimSpace(detail) == "" {
		if *roleSatisfied {
			detail = "Caller-supplied role evidence satisfies the resource action."
		} else {
			detail = "Caller-supplied role evidence does not satisfy the resource action."
		}
	}
	return models.ResourceAccessRequirementResult{
		Kind:      models.ResourceAccessRequirementRole,
		Label:     label,
		Status:    status,
		Satisfied: *roleSatisfied,
		Present:   []string{fmt.Sprintf("role_satisfied:%t", *roleSatisfied)},
		Detail:    detail,
	}
}

func evaluateScopedSessionRequirement(label, requiredFor string, activeAllowedMarkings []string, scopedSessionActive bool, results []models.ResourceAccessMarkingResult) models.ResourceAccessRequirementResult {
	if strings.TrimSpace(label) == "" {
		label = "Scoped session"
	}
	if !scopedSessionActive {
		return notApplicableResourceRequirement(
			models.ResourceAccessRequirementScopedSession,
			label,
			"No active scoped-session marking subset was supplied; normal marking membership decides access.",
		)
	}
	required := []string{}
	present := []string{}
	missing := []string{}
	for _, result := range results {
		if !stringInSlice(result.RequiredFor, requiredFor) {
			continue
		}
		markingLabel := effectiveMarkingLabel(result.MarkingID, result.MarkingName)
		required = append(required, markingLabel)
		if result.ScopedSessionSatisfied {
			present = append(present, markingLabel)
		} else {
			missing = append(missing, markingLabel)
		}
	}
	if len(required) == 0 {
		return models.ResourceAccessRequirementResult{
			Kind:      models.ResourceAccessRequirementScopedSession,
			Label:     label,
			Status:    models.ResourceAccessRequirementStatusPassed,
			Satisfied: true,
			Present:   normalizeAccessStrings(activeAllowedMarkings),
			Detail:    "Active scoped session is present; the resource has no marking requirements outside it.",
		}
	}
	passed := len(missing) == 0
	status := models.ResourceAccessRequirementStatusFailed
	detail := "Active scoped session excludes one or more required markings."
	if passed {
		status = models.ResourceAccessRequirementStatusPassed
		detail = "Active scoped session includes every required resource and data marking."
	}
	return models.ResourceAccessRequirementResult{
		Kind:      models.ResourceAccessRequirementScopedSession,
		Label:     label,
		Status:    status,
		Satisfied: passed,
		Required:  required,
		Present:   present,
		Missing:   missing,
		Detail:    detail,
	}
}

func evaluateResourceMarkingRequirement(kind, label, requiredFor string, results []models.ResourceAccessMarkingResult) models.ResourceAccessRequirementResult {
	required := []string{}
	present := []string{}
	missing := []string{}
	sources := []string{}
	missingMembership := 0
	missingScopedSession := 0
	for _, result := range results {
		if !stringInSlice(result.RequiredFor, requiredFor) {
			continue
		}
		markingLabel := effectiveMarkingLabel(result.MarkingID, result.MarkingName)
		required = append(required, markingLabel)
		for _, source := range result.Sources {
			if source.RequiredFor == requiredFor {
				sources = appendUniqueStringSorted(sources, source.SourceKind)
			}
		}
		if result.Satisfied {
			present = append(present, markingLabel)
		} else {
			missing = append(missing, markingLabel)
			if !result.MembershipSatisfied {
				missingMembership++
			}
			if !result.ScopedSessionSatisfied {
				missingScopedSession++
			}
		}
	}
	if len(required) == 0 {
		return notApplicableResourceRequirement(kind, label, "No marking requirements of this type apply to the resource.")
	}
	passed := len(missing) == 0
	status := models.ResourceAccessRequirementStatusFailed
	detail := "Principal is missing one or more required markings."
	if passed {
		status = models.ResourceAccessRequirementStatusPassed
		detail = "Principal is a member of every required marking."
	} else if missingMembership == 0 && missingScopedSession > 0 {
		detail = "Principal is a member of the required markings, but the active scoped session excludes one or more of them."
	} else if missingMembership > 0 && missingScopedSession > 0 {
		detail = "Principal is missing marking membership and the active scoped session excludes one or more required markings."
	}
	return models.ResourceAccessRequirementResult{
		Kind:      kind,
		Label:     label,
		Status:    status,
		Satisfied: passed,
		Required:  required,
		Present:   present,
		Missing:   missing,
		Detail:    detail,
		Sources:   sources,
	}
}

func notApplicableResourceRequirement(kind, label, detail string) models.ResourceAccessRequirementResult {
	return models.ResourceAccessRequirementResult{
		Kind:      kind,
		Label:     label,
		Status:    models.ResourceAccessRequirementStatusNotApplicable,
		Satisfied: true,
		Detail:    detail,
	}
}

func resourceAccessRequirementsSatisfied(items []models.ResourceAccessRequirementResult) bool {
	for _, item := range items {
		if !item.Satisfied {
			return false
		}
	}
	return true
}

func effectiveSourceKind(relations []string) string {
	if len(relations) == 0 {
		return models.EffectiveResourceMarkingSourceDirect
	}
	hasHierarchy := false
	hasLineage := false
	for _, relation := range relations {
		switch relation {
		case models.ResourceMarkingRelationHierarchy:
			hasHierarchy = true
		case models.ResourceMarkingRelationLineage:
			hasLineage = true
		}
	}
	switch {
	case hasHierarchy && hasLineage:
		return models.EffectiveResourceMarkingSourceMixed
	case hasLineage:
		return models.EffectiveResourceMarkingSourceLineage
	default:
		return models.EffectiveResourceMarkingSourceHierarchy
	}
}

func effectiveRequiredFor(relations []string) string {
	for _, relation := range relations {
		if relation == models.ResourceMarkingRelationLineage {
			return models.ResourceMarkingRequiredForDataAccess
		}
	}
	return models.ResourceMarkingRequiredForResourceAccess
}

func pathHops(tokens, relations []string) []models.ResourceMarkingPathHop {
	out := make([]models.ResourceMarkingPathHop, 0, len(tokens))
	for idx, token := range tokens {
		kind, id := splitResourceToken(token)
		hop := models.ResourceMarkingPathHop{ResourceKind: kind, ResourceID: id}
		if idx > 0 && idx-1 < len(relations) {
			hop.RelationKind = relations[idx-1]
		}
		out = append(out, hop)
	}
	return out
}

func resourceToken(kind, id string) string {
	return strings.ToLower(strings.TrimSpace(kind)) + ":" + strings.TrimSpace(id)
}

func splitResourceToken(token string) (string, string) {
	kind, id, ok := strings.Cut(token, ":")
	if !ok {
		return "resource", token
	}
	return kind, id
}

func clampResourceMarkingDepth(depth int) int {
	if depth <= 0 {
		return defaultResourceMarkingInheritanceDepth
	}
	if depth > defaultResourceMarkingInheritanceDepth {
		return defaultResourceMarkingInheritanceDepth
	}
	return depth
}

func appendUniqueStringSorted(values []string, value string) []string {
	if strings.TrimSpace(value) == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	values = append(values, value)
	sort.Strings(values)
	return values
}

func stringInSlice(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func resourceMarkingAllowedByScope(item models.EffectiveResourceMarking, activeAllowedMarkings []string, scopedSessionActive bool) bool {
	if !scopedSessionActive {
		return true
	}
	allowed := markingAccessSet(activeAllowedMarkings)
	if len(allowed) == 0 {
		return false
	}
	if allowed[strings.ToLower(item.MarkingID.String())] {
		return true
	}
	return allowed[strings.ToLower(strings.TrimSpace(item.MarkingName))]
}

func markingAccessSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out[strings.ToLower(value)] = true
	}
	return out
}

func normalizeAccessStrings(values []string) []string {
	set := markingAccessSet(values)
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func uuidInSlice(values []uuid.UUID, want uuid.UUID) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func uuidRawStrings(values []uuid.UUID) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.String())
	}
	sort.Strings(out)
	return out
}

func missingAccessStrings(required, present []string) []string {
	presentSet := map[string]bool{}
	for _, value := range present {
		presentSet[strings.ToLower(value)] = true
	}
	out := []string{}
	for _, value := range required {
		if !presentSet[strings.ToLower(value)] {
			out = append(out, value)
		}
	}
	return out
}

func effectiveMarkingLabel(id uuid.UUID, name string) string {
	if strings.TrimSpace(name) != "" {
		return name + " (" + id.String() + ")"
	}
	return id.String()
}
