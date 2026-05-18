package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/rid"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
)

const (
	referenceRelationshipContains = "contains"
	referenceRelationshipProject  = "project_reference"
)

type ResourceReferenceNode struct {
	ResourceKind string     `json:"resource_kind"`
	ResourceID   uuid.UUID  `json:"resource_id"`
	ResourceRID  string     `json:"resource_rid"`
	DisplayName  string     `json:"display_name"`
	Description  *string    `json:"description,omitempty"`
	ProjectID    *uuid.UUID `json:"project_id,omitempty"`
	ProjectRID   *string    `json:"project_rid,omitempty"`
}

type ResourceReferenceEdge struct {
	Source       ResourceReferenceNode `json:"source"`
	Target       ResourceReferenceNode `json:"target"`
	Relationship string                `json:"relationship"`
	CreatedAt    time.Time             `json:"created_at"`
	UpdatedAt    time.Time             `json:"updated_at"`
	Derived      bool                  `json:"derived"`
}

type ResourceReferenceGraphResponse struct {
	ResourceKind string                  `json:"resource_kind"`
	ResourceID   uuid.UUID               `json:"resource_id"`
	ResourceRID  string                  `json:"resource_rid"`
	DependsOn    []ResourceReferenceEdge `json:"depends_on"`
	UsedBy       []ResourceReferenceEdge `json:"used_by"`
}

type ReplaceResourceReferencesRequest struct {
	DependsOn []ReferenceTargetInput `json:"depends_on"`
}

type ReferenceTargetInput struct {
	ResourceKind string    `json:"resource_kind"`
	ResourceID   uuid.UUID `json:"resource_id"`
	Relationship string    `json:"relationship,omitempty"`
}

type referenceRow struct {
	SourceKind   ResourceKind
	SourceID     uuid.UUID
	TargetKind   ResourceKind
	TargetID     uuid.UUID
	Relationship string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Derived      bool
}

func (h *Handlers) GetResourceReferences(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	kind, err := ParseResourceKind(chi.URLParam(r, "kind"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	resourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid resource id")
		return
	}
	accessible, err := domain.ListAccessibleProjects(r.Context(), h.Repo.Pool, claims)
	if err != nil {
		slog.Error("resource references access evaluation", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to evaluate resource visibility")
		return
	}
	graph, err := h.Repo.ResourceReferenceGraph(r.Context(), kind, resourceID, accessible)
	if err != nil {
		slog.Error("resource references", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to load resource references")
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

func (h *Handlers) ReplaceResourceReferences(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	kind, err := ParseResourceKind(chi.URLParam(r, "kind"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	resourceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid resource id")
		return
	}
	if status, msg := h.Repo.ensureCanRegisterReferences(r.Context(), claims, kind, resourceID); status != 0 {
		writeJSONErr(w, status, msg)
		return
	}
	var body ReplaceResourceReferencesRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(body.DependsOn) > 500 {
		writeJSONErr(w, http.StatusBadRequest, "at most 500 references per resource")
		return
	}
	edges := make([]ReferenceTargetInput, 0, len(body.DependsOn))
	for _, edge := range body.DependsOn {
		targetKind, err := ParseResourceKind(edge.ResourceKind)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if edge.ResourceID == uuid.Nil {
			writeJSONErr(w, http.StatusBadRequest, "target resource_id required")
			return
		}
		relationship := strings.TrimSpace(edge.Relationship)
		if relationship == "" {
			relationship = "depends_on"
		}
		if targetKind == kind && edge.ResourceID == resourceID {
			writeJSONErr(w, http.StatusBadRequest, "a resource cannot reference itself")
			return
		}
		edges = append(edges, ReferenceTargetInput{
			ResourceKind: string(targetKind),
			ResourceID:   edge.ResourceID,
			Relationship: relationship,
		})
	}
	if err := h.Repo.ReplaceResourceReferences(r.Context(), kind, resourceID, edges, claims.Sub); err != nil {
		slog.Error("replace resource references", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to replace resource references")
		return
	}
	graph, err := h.Repo.ResourceReferenceGraph(r.Context(), kind, resourceID, nil)
	if err != nil {
		slog.Error("resource references", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to load resource references")
		return
	}
	writeJSON(w, http.StatusOK, graph)
}

func (r *Repo) ResourceReferenceGraph(
	ctx context.Context,
	kind ResourceKind,
	resourceID uuid.UUID,
	accessible map[uuid.UUID]models.OntologyProjectRole,
) (ResourceReferenceGraphResponse, error) {
	accessibleProjectIDs := mapKeys(accessible)
	rows, err := r.resourceReferenceRows(ctx, kind, resourceID, accessibleProjectIDs)
	if err != nil {
		return ResourceReferenceGraphResponse{}, err
	}
	rows = dedupeReferenceRows(rows)

	out := ResourceReferenceGraphResponse{
		ResourceKind: string(kind),
		ResourceID:   resourceID,
		ResourceRID:  resourceRIDForKind(kind, resourceID),
		DependsOn:    []ResourceReferenceEdge{},
		UsedBy:       []ResourceReferenceEdge{},
	}
	for _, row := range rows {
		source, err := r.referenceNode(ctx, row.SourceKind, row.SourceID)
		if err != nil {
			return ResourceReferenceGraphResponse{}, err
		}
		target, err := r.referenceNode(ctx, row.TargetKind, row.TargetID)
		if err != nil {
			return ResourceReferenceGraphResponse{}, err
		}
		edge := ResourceReferenceEdge{
			Source:       source,
			Target:       target,
			Relationship: row.Relationship,
			CreatedAt:    row.CreatedAt,
			UpdatedAt:    row.UpdatedAt,
			Derived:      row.Derived,
		}
		if row.SourceKind == kind && row.SourceID == resourceID {
			out.DependsOn = append(out.DependsOn, edge)
		}
		if row.TargetKind == kind && row.TargetID == resourceID {
			out.UsedBy = append(out.UsedBy, edge)
		}
	}
	return out, nil
}

func (r *Repo) ReplaceResourceReferences(
	ctx context.Context,
	sourceKind ResourceKind,
	sourceID uuid.UUID,
	edges []ReferenceTargetInput,
	actor uuid.UUID,
) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	if _, err := tx.Exec(ctx,
		`DELETE FROM compass_resource_references
		  WHERE source_kind = $1 AND source_id = $2`,
		string(sourceKind), sourceID,
	); err != nil {
		return err
	}
	for _, edge := range edges {
		if _, err := tx.Exec(ctx,
			`INSERT INTO compass_resource_references
			     (source_kind, source_id, target_kind, target_id, relationship, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (source_kind, source_id, target_kind, target_id, relationship)
			 DO UPDATE SET updated_at = NOW(), created_by = EXCLUDED.created_by`,
			string(sourceKind), sourceID, edge.ResourceKind, edge.ResourceID,
			strings.TrimSpace(edge.Relationship), actor,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *Repo) resourceReferenceRows(ctx context.Context, kind ResourceKind, resourceID uuid.UUID, accessibleProjectIDs []uuid.UUID) ([]referenceRow, error) {
	out := make([]referenceRow, 0)
	rows, err := r.Pool.Query(ctx,
		`SELECT source_kind, source_id, target_kind, target_id, relationship, created_at, updated_at, FALSE AS derived
		   FROM compass_resource_references
		  WHERE (source_kind = $1 AND source_id = $2)
		     OR (target_kind = $1 AND target_id = $2)`,
		string(kind), resourceID,
	)
	if err != nil {
		return nil, err
	}
	explicitRows, err := scanReferenceRows(rows)
	if err != nil {
		return nil, err
	}
	out = append(out, explicitRows...)

	if len(accessibleProjectIDs) == 0 {
		return out, nil
	}

	rows, err = r.Pool.Query(ctx,
		`SELECT $4::text AS source_kind,
		        project_id AS source_id,
		        resource_kind AS target_kind,
		        resource_id AS target_id,
		        $5::text AS relationship,
		        created_at,
		        created_at AS updated_at,
		        TRUE AS derived
		   FROM ontology_project_resources
		  WHERE project_id = ANY($3::uuid[])
		    AND (
		        ($1 = $4 AND project_id = $2)
		        OR (resource_kind = $1 AND resource_id = $2)
		    )`,
		string(kind), resourceID, accessibleProjectIDs,
		string(ResourceOntologyProject), referenceRelationshipContains,
	)
	if err != nil {
		return nil, err
	}
	bindingRows, err := scanReferenceRows(rows)
	if err != nil {
		return nil, err
	}
	out = append(out, bindingRows...)

	rows, err = r.Pool.Query(ctx,
		`SELECT $4::text AS source_kind,
		        p.id AS source_id,
		        ref.kind AS target_kind,
		        ref.id AS target_id,
		        $5::text AS relationship,
		        p.created_at,
		        p.updated_at,
		        TRUE AS derived
		   FROM ontology_projects p
		   CROSS JOIN LATERAL jsonb_to_recordset(COALESCE(p."references", '[]'::jsonb))
		        AS ref(kind text, id uuid, label text)
		  WHERE p.id = ANY($3::uuid[])
		    AND (
		        ($1 = $4 AND p.id = $2)
		        OR (ref.kind = $1 AND ref.id = $2)
		    )`,
		string(kind), resourceID, accessibleProjectIDs,
		string(ResourceOntologyProject), referenceRelationshipProject,
	)
	if err != nil {
		return nil, err
	}
	projectReferenceRows, err := scanReferenceRows(rows)
	if err != nil {
		return nil, err
	}
	out = append(out, projectReferenceRows...)
	return out, nil
}

func scanReferenceRows(rows pgx.Rows) ([]referenceRow, error) {
	defer rows.Close()
	out := make([]referenceRow, 0)
	for rows.Next() {
		var rawSourceKind, rawTargetKind string
		var row referenceRow
		if err := rows.Scan(
			&rawSourceKind,
			&row.SourceID,
			&rawTargetKind,
			&row.TargetID,
			&row.Relationship,
			&row.CreatedAt,
			&row.UpdatedAt,
			&row.Derived,
		); err != nil {
			return nil, err
		}
		sourceKind, err := ParseResourceKind(rawSourceKind)
		if err != nil {
			continue
		}
		targetKind, err := ParseResourceKind(rawTargetKind)
		if err != nil {
			continue
		}
		row.SourceKind = sourceKind
		row.TargetKind = targetKind
		out = append(out, row)
	}
	return out, rows.Err()
}

func dedupeReferenceRows(rows []referenceRow) []referenceRow {
	out := make([]referenceRow, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		key := fmt.Sprintf("%s:%s>%s:%s:%s", row.SourceKind, row.SourceID, row.TargetKind, row.TargetID, row.Relationship)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, row)
	}
	return out
}

func (r *Repo) referenceNode(ctx context.Context, kind ResourceKind, resourceID uuid.UUID) (ResourceReferenceNode, error) {
	node := ResourceReferenceNode{
		ResourceKind: string(kind),
		ResourceID:   resourceID,
		ResourceRID:  resourceRIDForKind(kind, resourceID),
		DisplayName:  fallbackReferenceLabel(kind, resourceID),
	}
	switch kind {
	case ResourceOntologyProject:
		labels, err := r.ResolveProjectLabels(ctx, []uuid.UUID{resourceID})
		if err != nil {
			return node, err
		}
		if label, ok := labels[resourceID]; ok {
			node.DisplayName = label.label
			node.Description = label.description
			node.ProjectID = &resourceID
			projectRID := models.ProjectRIDFromID(resourceID)
			node.ProjectRID = &projectRID
		}
	case ResourceOntologyFolder:
		labels, err := r.ResolveFolderLabels(ctx, []uuid.UUID{resourceID})
		if err != nil {
			return node, err
		}
		if label, ok := labels[resourceID]; ok {
			node.DisplayName = label.label
			node.Description = label.description
		}
		var projectID uuid.UUID
		err = r.Pool.QueryRow(ctx,
			`SELECT project_id FROM ontology_project_folders WHERE id = $1`,
			resourceID,
		).Scan(&projectID)
		if err == nil {
			node.ProjectID = &projectID
			projectRID := models.ProjectRIDFromID(projectID)
			node.ProjectRID = &projectRID
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return node, err
		}
	case ResourceOntologyResourceBinding:
		var (
			projectID    uuid.UUID
			resourceKind string
		)
		err := r.Pool.QueryRow(ctx,
			`SELECT project_id, resource_kind
			   FROM ontology_project_resources
			  WHERE resource_id = $1`,
			resourceID,
		).Scan(&projectID, &resourceKind)
		if err == nil {
			node.ProjectID = &projectID
			projectRID := models.ProjectRIDFromID(projectID)
			node.ProjectRID = &projectRID
			node.DisplayName = resourceKind + " " + shortReferenceID(resourceID)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return node, err
		}
	}
	return node, nil
}

func (r *Repo) ensureCanRegisterReferences(ctx context.Context, claims *authmw.Claims, kind ResourceKind, resourceID uuid.UUID) (int, string) {
	if claims.HasRole("admin") {
		return 0, ""
	}
	switch kind {
	case ResourceOntologyProject, ResourceOntologyFolder, ResourceOntologyResourceBinding:
		return r.ensureOwnerOrAdmin(ctx, claims, kind, resourceID)
	default:
		return http.StatusForbidden, "only an admin may register references for externally owned resources"
	}
}

func resourceRIDForKind(kind ResourceKind, id uuid.UUID) string {
	switch kind {
	case ResourceOntologyProject:
		return models.ProjectRIDFromID(id)
	case ResourceOntologyFolder:
		return models.FolderRIDFromID(id)
	case ResourceOntologyResourceBinding:
		return rid.MustNewUUID("compass", rid.DefaultInstance, "resource-binding", id).String()
	case ResourceDataset:
		return rid.MustNewUUID("foundry", rid.DefaultInstance, "dataset", id).String()
	case ResourcePipeline:
		return rid.MustNewUUID("foundry", rid.DefaultInstance, "pipeline", id).String()
	case ResourceQuery:
		return rid.MustNewUUID("foundry", rid.DefaultInstance, "query", id).String()
	case ResourceNotebook:
		return rid.MustNewUUID("foundry", rid.DefaultInstance, "notebook", id).String()
	case ResourceApp:
		return rid.MustNewUUID("foundry", rid.DefaultInstance, "app", id).String()
	case ResourceDashboard:
		return rid.MustNewUUID("foundry", rid.DefaultInstance, "dashboard", id).String()
	case ResourceReport:
		return rid.MustNewUUID("foundry", rid.DefaultInstance, "report", id).String()
	case ResourceModel:
		return rid.MustNewUUID("foundry", rid.DefaultInstance, "model", id).String()
	case ResourceWorkflow:
		return rid.MustNewUUID("foundry", rid.DefaultInstance, "workflow", id).String()
	default:
		return rid.MustNewUUID("openfoundry", rid.DefaultInstance, "resource", id).String()
	}
}

func fallbackReferenceLabel(kind ResourceKind, id uuid.UUID) string {
	return strings.TrimSpace(strings.ReplaceAll(string(kind), "_", " ")) + " " + shortReferenceID(id)
}

func shortReferenceID(id uuid.UUID) string {
	return id.String()[:8]
}

func mapKeys[V any](values map[uuid.UUID]V) []uuid.UUID {
	if len(values) == 0 {
		return nil
	}
	out := make([]uuid.UUID, 0, len(values))
	for id := range values {
		out = append(out, id)
	}
	return out
}
