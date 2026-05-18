package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/libs/outbox"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
)

const (
	ResourceSearchTopic         = "compass.resource.search.updated.v1"
	ResourceSearchEventCreated  = "compass.resource.created.v1"
	ResourceSearchEventUpdated  = "compass.resource.updated.v1"
	ResourceSearchEventMoved    = "compass.resource.moved.v1"
	ResourceSearchEventTrashed  = "compass.resource.trashed.v1"
	ResourceSearchEventRestored = "compass.resource.restored.v1"
	ResourceSearchEventPurged   = "compass.resource.purged.v1"
	ResourceSearchTypeProject   = "project"
	ResourceSearchTypeFolder    = "folder"
)

// ResourceSearchEntry is the Compass catalog document projected for search.
// It intentionally carries only cross-resource metadata; per-type details stay
// in the owning resource service and are opened through OpenURL/RID.
type ResourceSearchEntry struct {
	ResourceRID      string     `json:"rid"`
	ResourceType     string     `json:"type"`
	DisplayName      string     `json:"display_name"`
	OwningProjectID  *uuid.UUID `json:"owning_project_id,omitempty"`
	OwningProjectRID *string    `json:"owning_project_rid,omitempty"`
	OrganizationRIDs []string   `json:"organization_rids"`
	MarkingRIDs      []string   `json:"marking_rids"`
	LastModifiedAt   time.Time  `json:"last_modified_at"`
	OwnerID          *uuid.UUID `json:"owner_id,omitempty"`
	Tags             []string   `json:"tags"`
	Summary          string     `json:"summary"`
	OpenURL          string     `json:"open_url"`
	IsDeleted        bool       `json:"is_deleted"`
}

type resourceSearchEvent struct {
	EventType  string              `json:"event_type"`
	Resource   ResourceSearchEntry `json:"resource"`
	OccurredAt time.Time           `json:"occurred_at"`
}

func UpsertProjectSearchIndexTx(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, eventType string) error {
	entry, err := loadProjectSearchEntryTx(ctx, tx, projectID)
	if err != nil {
		return err
	}
	if entry == nil {
		return nil
	}
	return upsertResourceSearchEntryTx(ctx, tx, *entry, eventType)
}

func UpsertFolderSearchIndexTx(ctx context.Context, tx pgx.Tx, folderID uuid.UUID, eventType string) error {
	entry, err := loadFolderSearchEntryTx(ctx, tx, folderID)
	if err != nil {
		return err
	}
	if entry == nil {
		return nil
	}
	return upsertResourceSearchEntryTx(ctx, tx, *entry, eventType)
}

func DeleteProjectSearchIndexTx(ctx context.Context, tx pgx.Tx, projectID uuid.UUID, eventType string) error {
	rows, err := tx.Query(ctx,
		`SELECT resource_rid, resource_type, display_name, owning_project_id,
		        owning_project_rid, organization_rids, marking_rids,
		        last_modified_at, owner_id, tags, summary, open_url, is_deleted
		   FROM compass_resource_search_index
		  WHERE owning_project_id = $1
		  ORDER BY resource_type DESC`,
		projectID,
	)
	if err != nil {
		return err
	}
	entries, err := scanResourceSearchEntries(rows)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		rid := models.ProjectRIDFromID(projectID)
		entries = []ResourceSearchEntry{{
			ResourceRID:    rid,
			ResourceType:   ResourceSearchTypeProject,
			DisplayName:    rid,
			LastModifiedAt: time.Now().UTC(),
			IsDeleted:      true,
		}}
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM compass_resource_search_index WHERE owning_project_id = $1`,
		projectID,
	); err != nil {
		return err
	}
	for _, entry := range entries {
		entry.IsDeleted = true
		if err := enqueueResourceSearchEventTx(ctx, tx, entry, eventType); err != nil {
			return err
		}
	}
	return nil
}

func DeleteFolderSearchIndexTx(ctx context.Context, tx pgx.Tx, folderID uuid.UUID, eventType string) error {
	rid := models.FolderRIDFromID(folderID)
	entry, err := loadResourceSearchEntryByRIDTx(ctx, tx, rid)
	if err != nil {
		return err
	}
	if entry == nil {
		entry = &ResourceSearchEntry{
			ResourceRID:    rid,
			ResourceType:   ResourceSearchTypeFolder,
			DisplayName:    rid,
			LastModifiedAt: time.Now().UTC(),
			IsDeleted:      true,
		}
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM compass_resource_search_index WHERE resource_rid = $1`,
		rid,
	); err != nil {
		return err
	}
	entry.IsDeleted = true
	return enqueueResourceSearchEventTx(ctx, tx, *entry, eventType)
}

func upsertResourceSearchEntryTx(ctx context.Context, tx pgx.Tx, entry ResourceSearchEntry, eventType string) error {
	entry.Normalize()
	organizationJSON, err := json.Marshal(entry.OrganizationRIDs)
	if err != nil {
		return fmt.Errorf("encode resource search organizations: %w", err)
	}
	markingJSON, err := json.Marshal(entry.MarkingRIDs)
	if err != nil {
		return fmt.Errorf("encode resource search markings: %w", err)
	}
	tagsJSON, err := json.Marshal(entry.Tags)
	if err != nil {
		return fmt.Errorf("encode resource search tags: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO compass_resource_search_index (
		     resource_rid, resource_type, display_name, owning_project_id,
		     owning_project_rid, organization_rids, marking_rids,
		     last_modified_at, owner_id, tags, summary, open_url, is_deleted, indexed_at
		 )
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10::jsonb, $11, $12, $13, NOW())
		 ON CONFLICT (resource_rid) DO UPDATE SET
		     resource_type = EXCLUDED.resource_type,
		     display_name = EXCLUDED.display_name,
		     owning_project_id = EXCLUDED.owning_project_id,
		     owning_project_rid = EXCLUDED.owning_project_rid,
		     organization_rids = EXCLUDED.organization_rids,
		     marking_rids = EXCLUDED.marking_rids,
		     last_modified_at = EXCLUDED.last_modified_at,
		     owner_id = EXCLUDED.owner_id,
		     tags = EXCLUDED.tags,
		     summary = EXCLUDED.summary,
		     open_url = EXCLUDED.open_url,
		     is_deleted = EXCLUDED.is_deleted,
		     indexed_at = NOW()`,
		entry.ResourceRID, entry.ResourceType, entry.DisplayName,
		entry.OwningProjectID, entry.OwningProjectRID, string(organizationJSON),
		string(markingJSON), entry.LastModifiedAt, entry.OwnerID, string(tagsJSON),
		entry.Summary, entry.OpenURL, entry.IsDeleted,
	); err != nil {
		return err
	}
	return enqueueResourceSearchEventTx(ctx, tx, entry, eventType)
}

func enqueueResourceSearchEventTx(ctx context.Context, tx pgx.Tx, entry ResourceSearchEntry, eventType string) error {
	entry.Normalize()
	event := resourceSearchEvent{
		EventType:  eventType,
		Resource:   entry,
		OccurredAt: time.Now().UTC(),
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode resource search event: %w", err)
	}
	basis := fmt.Sprintf("%s|%s|%d|%t", eventType, entry.ResourceRID, entry.LastModifiedAt.UnixNano(), entry.IsDeleted)
	eventID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("openfoundry:compass:resource-search:"+basis))
	return outbox.Enqueue(ctx, tx, outbox.New(
		eventID,
		"compass_resource",
		entry.ResourceRID,
		ResourceSearchTopic,
		payload,
	).WithHeader("event_type", eventType).
		WithHeader("resource_rid", entry.ResourceRID).
		WithHeader("resource_type", entry.ResourceType))
}

func loadProjectSearchEntryTx(ctx context.Context, tx pgx.Tx, projectID uuid.UUID) (*ResourceSearchEntry, error) {
	row := tx.QueryRow(ctx,
		`SELECT COALESCE(rid, 'ri.compass.main.project.' || id::text),
		        display_name,
		        id,
		        COALESCE(rid, 'ri.compass.main.project.' || id::text),
		        COALESCE(organization_rids, '[]'::jsonb),
		        COALESCE(marking_rids, '[]'::jsonb),
		        updated_at,
		        owner_id,
		        COALESCE(description, ''),
		        '/projects/' || COALESCE(rid, 'ri.compass.main.project.' || id::text),
		        COALESCE(is_deleted, FALSE)
		   FROM ontology_projects
		  WHERE id = $1`,
		projectID,
	)
	var (
		entry      ResourceSearchEntry
		projectIDV uuid.UUID
		projectRID string
		orgJSON    []byte
		markJSON   []byte
	)
	if err := row.Scan(
		&entry.ResourceRID, &entry.DisplayName, &projectIDV, &projectRID,
		&orgJSON, &markJSON, &entry.LastModifiedAt, &entry.OwnerID,
		&entry.Summary, &entry.OpenURL, &entry.IsDeleted,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	entry.ResourceType = ResourceSearchTypeProject
	entry.OwningProjectID = &projectIDV
	entry.OwningProjectRID = &projectRID
	entry.OrganizationRIDs = decodeStringArrayJSON(orgJSON)
	entry.MarkingRIDs = decodeStringArrayJSON(markJSON)
	entry.Tags = []string{}
	entry.Normalize()
	return &entry, nil
}

func loadFolderSearchEntryTx(ctx context.Context, tx pgx.Tx, folderID uuid.UUID) (*ResourceSearchEntry, error) {
	row := tx.QueryRow(ctx,
		`SELECT COALESCE(f.rid, 'ri.compass.main.folder.' || f.id::text),
		        f.name,
		        f.project_id,
		        COALESCE(p.rid, 'ri.compass.main.project.' || p.id::text),
		        COALESCE(p.organization_rids, '[]'::jsonb),
		        COALESCE(p.marking_rids, '[]'::jsonb),
		        COALESCE(f.view_requirement_marking_rids, '[]'::jsonb),
		        f.updated_at,
		        f.created_by,
		        COALESCE(f.description, ''),
		        '/projects/' || COALESCE(p.rid, 'ri.compass.main.project.' || p.id::text) ||
		            '/folders/' || COALESCE(f.rid, 'ri.compass.main.folder.' || f.id::text),
		        COALESCE(f.is_deleted, FALSE)
		   FROM ontology_project_folders f
		   JOIN ontology_projects p ON p.id = f.project_id
		  WHERE f.id = $1`,
		folderID,
	)
	var (
		entry       ResourceSearchEntry
		projectID   uuid.UUID
		projectRID  string
		orgJSON     []byte
		markJSON    []byte
		viewReqJSON []byte
	)
	if err := row.Scan(
		&entry.ResourceRID, &entry.DisplayName, &projectID, &projectRID,
		&orgJSON, &markJSON, &viewReqJSON, &entry.LastModifiedAt, &entry.OwnerID,
		&entry.Summary, &entry.OpenURL, &entry.IsDeleted,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	entry.ResourceType = ResourceSearchTypeFolder
	entry.OwningProjectID = &projectID
	entry.OwningProjectRID = &projectRID
	entry.OrganizationRIDs = decodeStringArrayJSON(orgJSON)
	entry.MarkingRIDs = normalizeStringSlice(append(
		decodeStringArrayJSON(markJSON),
		decodeStringArrayJSON(viewReqJSON)...,
	))
	entry.Tags = []string{}
	entry.Normalize()
	return &entry, nil
}

func loadResourceSearchEntryByRIDTx(ctx context.Context, tx pgx.Tx, resourceRID string) (*ResourceSearchEntry, error) {
	row := tx.QueryRow(ctx,
		`SELECT resource_rid, resource_type, display_name, owning_project_id,
		        owning_project_rid, organization_rids, marking_rids,
		        last_modified_at, owner_id, tags, summary, open_url, is_deleted
		   FROM compass_resource_search_index
		  WHERE resource_rid = $1`,
		resourceRID,
	)
	return scanResourceSearchEntry(row)
}

type resourceSearchScannable interface {
	Scan(dest ...any) error
}

func scanResourceSearchEntries(rows pgx.Rows) ([]ResourceSearchEntry, error) {
	defer rows.Close()
	entries := make([]ResourceSearchEntry, 0)
	for rows.Next() {
		entry, err := scanResourceSearchEntry(rows)
		if err != nil {
			return nil, err
		}
		if entry != nil {
			entries = append(entries, *entry)
		}
	}
	return entries, rows.Err()
}

func scanResourceSearchEntry(row resourceSearchScannable) (*ResourceSearchEntry, error) {
	var (
		entry    ResourceSearchEntry
		orgJSON  []byte
		markJSON []byte
		tagsJSON []byte
	)
	if err := row.Scan(
		&entry.ResourceRID, &entry.ResourceType, &entry.DisplayName,
		&entry.OwningProjectID, &entry.OwningProjectRID, &orgJSON,
		&markJSON, &entry.LastModifiedAt, &entry.OwnerID, &tagsJSON,
		&entry.Summary, &entry.OpenURL, &entry.IsDeleted,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	entry.OrganizationRIDs = decodeStringArrayJSON(orgJSON)
	entry.MarkingRIDs = decodeStringArrayJSON(markJSON)
	entry.Tags = decodeStringArrayJSON(tagsJSON)
	entry.Normalize()
	return &entry, nil
}

func (e *ResourceSearchEntry) Normalize() {
	e.ResourceRID = strings.TrimSpace(e.ResourceRID)
	e.ResourceType = strings.TrimSpace(e.ResourceType)
	e.DisplayName = strings.TrimSpace(e.DisplayName)
	if e.DisplayName == "" {
		e.DisplayName = e.ResourceRID
	}
	if e.OrganizationRIDs == nil {
		e.OrganizationRIDs = []string{}
	}
	e.OrganizationRIDs = normalizeStringSlice(e.OrganizationRIDs)
	if e.MarkingRIDs == nil {
		e.MarkingRIDs = []string{}
	}
	e.MarkingRIDs = normalizeStringSlice(e.MarkingRIDs)
	if e.Tags == nil {
		e.Tags = []string{}
	}
	e.Tags = normalizeStringSlice(e.Tags)
	e.Summary = strings.TrimSpace(e.Summary)
	e.OpenURL = strings.TrimSpace(e.OpenURL)
	if e.OpenURL == "" && e.ResourceRID != "" {
		e.OpenURL = "/resources/" + e.ResourceRID
	}
	if e.LastModifiedAt.IsZero() {
		e.LastModifiedAt = time.Now().UTC()
	}
}

func decodeStringArrayJSON(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return []string{}
	}
	return normalizeStringSlice(values)
}

func normalizeStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
