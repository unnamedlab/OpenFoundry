package workspace

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
)

const workspaceAuditSourceService = "tenancy-organizations-service"

var ErrTrashRetentionActive = errors.New("resource is still within trash retention window")

type trashPurgeSnapshot struct {
	Kind               ResourceKind
	ResourceID         uuid.UUID
	ResourceRID        string
	ResourceType       string
	ProjectRID         string
	MarkingsAtEvent    []string
	DisplayName        string
	DeletedAt          time.Time
	DeletedBy          *uuid.UUID
	RetentionDays      int
	PurgeAfter         *time.Time
	AffectedDependents []audittrail.AffectedDependent
}

func auditCtxFromWorkspaceRequest(claims *authmw.Claims, r *http.Request) audittrail.AuditContext {
	requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
	if requestID == "" {
		requestID = uuid.New().String()
	}
	return audittrail.AuditContext{
		ActorID:       claims.Sub.String(),
		IP:            workspaceClientIP(r),
		UserAgent:     r.Header.Get("User-Agent"),
		RequestID:     requestID,
		CorrelationID: r.Header.Get("X-Correlation-Id"),
		SourceService: workspaceAuditSourceService,
	}
}

func workspaceClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first := strings.SplitN(xff, ",", 2)[0]; strings.TrimSpace(first) != "" {
			return strings.TrimSpace(first)
		}
	}
	return r.Header.Get("X-Real-Ip")
}

func (s trashPurgeSnapshot) purgeMode(now time.Time, isAdmin bool) (string, error) {
	if s.PurgeAfter != nil && now.Before(s.PurgeAfter.UTC()) {
		if !isAdmin {
			return "", ErrTrashRetentionActive
		}
		return "admin_override", nil
	}
	return "retention_expired", nil
}

func (s trashPurgeSnapshot) auditEvent(purgedBy, purgeMode string) audittrail.AuditEvent {
	if strings.TrimSpace(purgedBy) == "" {
		purgedBy = "system"
	}
	return audittrail.NewCompassResourcePurged(
		s.ResourceRID,
		s.ProjectRID,
		s.MarkingsAtEvent,
		s.ResourceType,
		s.DisplayName,
		formatAuditTime(s.DeletedAt),
		uuidPtrString(s.DeletedBy),
		purgedBy,
		s.RetentionDays,
		formatAuditTimePtr(s.PurgeAfter),
		purgeMode,
		s.AffectedDependents,
		false,
	)
}

func (r *Repo) loadTrashPurgeSnapshotTx(ctx context.Context, tx pgx.Tx, kind ResourceKind, resourceID uuid.UUID) (*trashPurgeSnapshot, error) {
	switch kind {
	case ResourceOntologyProject:
		return r.loadProjectPurgeSnapshotTx(ctx, tx, resourceID)
	case ResourceOntologyFolder:
		return r.loadFolderPurgeSnapshotTx(ctx, tx, resourceID)
	case ResourceOntologyResourceBinding:
		return r.loadResourceBindingPurgeSnapshotTx(ctx, tx, resourceID)
	default:
		return nil, fmt.Errorf("purge is not implemented for resource_kind '%s'", kind)
	}
}

func (r *Repo) loadProjectPurgeSnapshotTx(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) (*trashPurgeSnapshot, error) {
	snapshot := &trashPurgeSnapshot{
		Kind:         ResourceOntologyProject,
		ResourceID:   projectID,
		ResourceType: ResourceSearchTypeProject,
	}
	var markingJSON []byte
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(rid, 'ri.compass.main.project.' || id::text),
		        display_name,
		        COALESCE(marking_rids, '[]'::jsonb),
		        deleted_at,
		        deleted_by,
		        COALESCE(trash_retention_days, $2),
		        COALESCE(purge_after, deleted_at + (COALESCE(trash_retention_days, $2)::int * INTERVAL '1 day'))
		   FROM ontology_projects
		  WHERE id = $1 AND is_deleted = TRUE
		  FOR UPDATE`,
		projectID, DefaultTrashRetentionDays,
	).Scan(
		&snapshot.ResourceRID,
		&snapshot.DisplayName,
		&markingJSON,
		&snapshot.DeletedAt,
		&snapshot.DeletedBy,
		&snapshot.RetentionDays,
		&snapshot.PurgeAfter,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	snapshot.ProjectRID = snapshot.ResourceRID
	snapshot.MarkingsAtEvent = decodeStringArrayJSON(markingJSON)
	dependents, err := r.projectPurgeDependentsTx(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	surface, err := workspaceSurfaceDependentsTx(ctx, tx, string(ResourceOntologyProject), projectID)
	if err != nil {
		return nil, err
	}
	snapshot.AffectedDependents = append(dependents, surface...)
	return snapshot, nil
}

func (r *Repo) loadFolderPurgeSnapshotTx(ctx context.Context, tx pgx.Tx, folderID uuid.UUID) (*trashPurgeSnapshot, error) {
	snapshot := &trashPurgeSnapshot{
		Kind:         ResourceOntologyFolder,
		ResourceID:   folderID,
		ResourceType: ResourceSearchTypeFolder,
	}
	var markingJSON []byte
	err := tx.QueryRow(ctx,
		`SELECT COALESCE(f.rid, 'ri.compass.main.folder.' || f.id::text),
		        COALESCE(p.rid, 'ri.compass.main.project.' || p.id::text),
		        f.name,
		        COALESCE(p.marking_rids, '[]'::jsonb),
		        f.deleted_at,
		        f.deleted_by,
		        COALESCE(f.trash_retention_days, $2),
		        COALESCE(f.purge_after, f.deleted_at + (COALESCE(f.trash_retention_days, $2)::int * INTERVAL '1 day'))
		   FROM ontology_project_folders f
		   JOIN ontology_projects p ON p.id = f.project_id
		  WHERE f.id = $1 AND f.is_deleted = TRUE
		  FOR UPDATE OF f`,
		folderID, DefaultTrashRetentionDays,
	).Scan(
		&snapshot.ResourceRID,
		&snapshot.ProjectRID,
		&snapshot.DisplayName,
		&markingJSON,
		&snapshot.DeletedAt,
		&snapshot.DeletedBy,
		&snapshot.RetentionDays,
		&snapshot.PurgeAfter,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	snapshot.MarkingsAtEvent = decodeStringArrayJSON(markingJSON)
	dependents, err := r.folderPurgeDependentsTx(ctx, tx, folderID)
	if err != nil {
		return nil, err
	}
	surface, err := workspaceSurfaceDependentsTx(ctx, tx, string(ResourceOntologyFolder), folderID)
	if err != nil {
		return nil, err
	}
	snapshot.AffectedDependents = append(dependents, surface...)
	return snapshot, nil
}

func (r *Repo) loadResourceBindingPurgeSnapshotTx(ctx context.Context, tx pgx.Tx, resourceID uuid.UUID) (*trashPurgeSnapshot, error) {
	snapshot := &trashPurgeSnapshot{
		Kind:         ResourceOntologyResourceBinding,
		ResourceID:   resourceID,
		ResourceRID:  resourceBindingRIDFromID(resourceID),
		ResourceType: "resource_binding",
	}
	var (
		resourceKind string
		markingJSON  []byte
	)
	err := tx.QueryRow(ctx,
		`SELECT r.resource_kind,
		        COALESCE(p.rid, 'ri.compass.main.project.' || p.id::text),
		        COALESCE(p.marking_rids, '[]'::jsonb),
		        r.deleted_at,
		        r.deleted_by,
		        COALESCE(r.trash_retention_days, $2),
		        COALESCE(r.purge_after, r.deleted_at + (COALESCE(r.trash_retention_days, $2)::int * INTERVAL '1 day'))
		   FROM ontology_project_resources r
		   JOIN ontology_projects p ON p.id = r.project_id
		  WHERE r.resource_id = $1 AND r.is_deleted = TRUE
		  FOR UPDATE OF r`,
		resourceID, DefaultTrashRetentionDays,
	).Scan(
		&resourceKind,
		&snapshot.ProjectRID,
		&markingJSON,
		&snapshot.DeletedAt,
		&snapshot.DeletedBy,
		&snapshot.RetentionDays,
		&snapshot.PurgeAfter,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	snapshot.DisplayName = resourceKind + ":" + resourceID.String()
	snapshot.MarkingsAtEvent = decodeStringArrayJSON(markingJSON)
	surface, err := workspaceSurfaceDependentsTx(ctx, tx, string(ResourceOntologyResourceBinding), resourceID)
	if err != nil {
		return nil, err
	}
	snapshot.AffectedDependents = surface
	return snapshot, nil
}

func (r *Repo) projectPurgeDependentsTx(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) ([]audittrail.AffectedDependent, error) {
	dependents := make([]audittrail.AffectedDependent, 0)
	rows, err := tx.Query(ctx,
		`SELECT id
		   FROM ontology_project_folders
		  WHERE project_id = $1
		  ORDER BY created_at ASC, id ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	folderIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		folderIDs = append(folderIDs, id)
		dependents = append(dependents, audittrail.AffectedDependent{
			Kind:         "folder",
			RID:          models.FolderRIDFromID(id),
			ID:           id.String(),
			Relationship: "project_child",
			Action:       "cascade_delete",
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, folderID := range folderIDs {
		surface, err := workspaceSurfaceDependentsTx(ctx, tx, string(ResourceOntologyFolder), folderID)
		if err != nil {
			return nil, err
		}
		dependents = append(dependents, surface...)
	}

	rows, err = tx.Query(ctx,
		`SELECT resource_kind, resource_id
		   FROM ontology_project_resources
		  WHERE project_id = $1
		  ORDER BY resource_kind ASC, resource_id ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			kind string
			id   uuid.UUID
		)
		if err := rows.Scan(&kind, &id); err != nil {
			rows.Close()
			return nil, err
		}
		dependents = append(dependents, audittrail.AffectedDependent{
			Kind:         "resource_binding",
			ID:           kind + ":" + id.String(),
			Relationship: "project_child",
			Action:       "cascade_delete",
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = tx.Query(ctx,
		`SELECT user_id, role
		   FROM ontology_project_memberships
		  WHERE project_id = $1
		  ORDER BY role ASC, user_id ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			userID uuid.UUID
			role   string
		)
		if err := rows.Scan(&userID, &role); err != nil {
			rows.Close()
			return nil, err
		}
		dependents = append(dependents, audittrail.AffectedDependent{
			Kind:         "project_membership",
			ID:           userID.String() + ":" + role,
			Relationship: "project_access",
			Action:       "cascade_delete",
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = tx.Query(ctx,
		`SELECT id
		   FROM ontology_project_resource_grants
		  WHERE project_id = $1
		  ORDER BY created_at ASC, id ASC`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		dependents = append(dependents, audittrail.AffectedDependent{
			Kind:         "resource_grant",
			ID:           id.String(),
			Relationship: "project_access",
			Action:       "cascade_delete",
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	searchDependents, err := searchIndexDependentsTx(ctx, tx, projectID, "")
	if err != nil {
		return nil, err
	}
	dependents = append(dependents, searchDependents...)
	return dependents, nil
}

func (r *Repo) folderPurgeDependentsTx(ctx context.Context, tx pgx.Tx, folderID uuid.UUID) ([]audittrail.AffectedDependent, error) {
	dependents := make([]audittrail.AffectedDependent, 0)
	rows, err := tx.Query(ctx,
		`SELECT id
		   FROM ontology_project_folders
		  WHERE parent_folder_id = $1
		  ORDER BY created_at ASC, id ASC`,
		folderID,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		dependents = append(dependents, audittrail.AffectedDependent{
			Kind:         "folder",
			RID:          models.FolderRIDFromID(id),
			ID:           id.String(),
			Relationship: "child_folder",
			Action:       "reparent_to_project_root",
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = tx.Query(ctx,
		`SELECT id
		   FROM ontology_project_resource_grants
		  WHERE scope_kind = 'folder' AND scope_id = $1
		  ORDER BY created_at ASC, id ASC`,
		folderID,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		dependents = append(dependents, audittrail.AffectedDependent{
			Kind:         "resource_grant",
			ID:           id.String(),
			Relationship: "folder_access",
			Action:       "delete",
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	searchDependents, err := searchIndexDependentsTx(ctx, tx, uuid.Nil, models.FolderRIDFromID(folderID))
	if err != nil {
		return nil, err
	}
	dependents = append(dependents, searchDependents...)
	return dependents, nil
}

func workspaceSurfaceDependentsTx(ctx context.Context, tx pgx.Tx, kind string, resourceID uuid.UUID) ([]audittrail.AffectedDependent, error) {
	dependents := make([]audittrail.AffectedDependent, 0)
	rows, err := tx.Query(ctx,
		`SELECT user_id
		   FROM user_favorites
		  WHERE resource_kind = $1 AND resource_id = $2
		  ORDER BY user_id ASC`,
		kind, resourceID,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var userID uuid.UUID
		if err := rows.Scan(&userID); err != nil {
			rows.Close()
			return nil, err
		}
		dependents = append(dependents, audittrail.AffectedDependent{
			Kind:         "favorite",
			ID:           userID.String(),
			Relationship: "user_favorite",
			Action:       "delete",
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = tx.Query(ctx,
		`SELECT id, user_id
		   FROM resource_access_log
		  WHERE resource_kind = $1 AND resource_id = $2
		  ORDER BY accessed_at ASC, id ASC`,
		kind, resourceID,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			id     uuid.UUID
			userID uuid.UUID
		)
		if err := rows.Scan(&id, &userID); err != nil {
			rows.Close()
			return nil, err
		}
		dependents = append(dependents, audittrail.AffectedDependent{
			Kind:         "recent_access",
			ID:           id.String() + ":" + userID.String(),
			Relationship: "resource_recent",
			Action:       "delete",
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = tx.Query(ctx,
		`SELECT id
		   FROM resource_shares
		  WHERE resource_kind = $1 AND resource_id = $2
		  ORDER BY created_at ASC, id ASC`,
		kind, resourceID,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		dependents = append(dependents, audittrail.AffectedDependent{
			Kind:         "resource_share",
			ID:           id.String(),
			Relationship: "direct_share",
			Action:       "delete",
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	return dependents, nil
}

func searchIndexDependentsTx(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, resourceRID string) ([]audittrail.AffectedDependent, error) {
	dependents := make([]audittrail.AffectedDependent, 0)
	var (
		rows pgx.Rows
		err  error
	)
	if resourceRID != "" {
		rows, err = tx.Query(ctx,
			`SELECT resource_rid, resource_type
			   FROM compass_resource_search_index
			  WHERE resource_rid = $1
			  ORDER BY resource_rid ASC`,
			resourceRID,
		)
	} else {
		rows, err = tx.Query(ctx,
			`SELECT resource_rid, resource_type
			   FROM compass_resource_search_index
			  WHERE owning_project_id = $1
			  ORDER BY resource_type ASC, resource_rid ASC`,
			projectID,
		)
	}
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			rid          string
			resourceType string
		)
		if err := rows.Scan(&rid, &resourceType); err != nil {
			rows.Close()
			return nil, err
		}
		dependents = append(dependents, audittrail.AffectedDependent{
			Kind:         "search_index_entry",
			RID:          rid,
			ID:           resourceType,
			Relationship: "catalog_projection",
			Action:       "delete",
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	return dependents, nil
}

func deleteWorkspaceSurfaceDependentsTx(ctx context.Context, tx pgx.Tx, kind string, resourceID uuid.UUID) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM user_favorites WHERE resource_kind = $1 AND resource_id = $2`,
		kind, resourceID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM resource_access_log WHERE resource_kind = $1 AND resource_id = $2`,
		kind, resourceID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM resource_shares WHERE resource_kind = $1 AND resource_id = $2`,
		kind, resourceID,
	); err != nil {
		return err
	}
	return nil
}

func deleteProjectChildWorkspaceSurfaceDependentsTx(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM user_favorites
		  WHERE resource_kind = $1
		    AND resource_id IN (
		          SELECT id FROM ontology_project_folders WHERE project_id = $2
		    )`,
		string(ResourceOntologyFolder), projectID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM resource_access_log
		  WHERE resource_kind = $1
		    AND resource_id IN (
		          SELECT id FROM ontology_project_folders WHERE project_id = $2
		    )`,
		string(ResourceOntologyFolder), projectID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM resource_shares
		  WHERE resource_kind = $1
		    AND resource_id IN (
		          SELECT id FROM ontology_project_folders WHERE project_id = $2
		    )`,
		string(ResourceOntologyFolder), projectID,
	); err != nil {
		return err
	}
	return nil
}

func deleteFolderScopedGrantsTx(ctx context.Context, tx pgx.Tx, folderID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`DELETE FROM ontology_project_resource_grants
		  WHERE scope_kind = 'folder' AND scope_id = $1`,
		folderID,
	)
	return err
}

func resourceBindingRIDFromID(id uuid.UUID) string {
	return "ri.compass.main.resource_binding." + id.String()
}

func formatAuditTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func formatAuditTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return formatAuditTime(*t)
}

func uuidPtrString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
