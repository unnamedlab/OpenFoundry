package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/models"
)

type catalogNamespaceStore interface {
	FetchNamespaceByName(ctx context.Context, projectRID string, path []string) (*models.IcebergNamespace, error)
	UpdateNamespaceProperties(ctx context.Context, id uuid.UUID, properties []byte) (*models.IcebergNamespace, error)
	DeleteNamespace(ctx context.Context, id uuid.UUID) (bool, error)
	CreateNamespace(ctx context.Context, body *models.CreateNamespaceRequest, createdBy uuid.UUID) (*models.IcebergNamespace, error)
}

type alterSchemaStore interface {
	UpdateTableSchema(ctx context.Context, tableID uuid.UUID, schema json.RawMessage) (*models.IcebergTable, error)
}

type adminStore interface {
	ListAdminTables(ctx context.Context, query models.ListIcebergTablesQuery) ([]models.IcebergTableSummary, error)
	GetTableByRID(ctx context.Context, rid string) (*models.IcebergTable, error)
}

type restNamespaceListResponse struct {
	Namespaces [][]string `json:"namespaces"`
}

type restCreateNamespaceRequest struct {
	Namespace  []string          `json:"namespace"`
	Properties map[string]string `json:"properties,omitempty"`
}

type restNamespaceResponse struct {
	Namespace  []string          `json:"namespace"`
	Properties map[string]string `json:"properties"`
}

type restUpdatePropertiesRequest struct {
	Removals []string          `json:"removals,omitempty"`
	Updates  map[string]string `json:"updates,omitempty"`
}

type restUpdatePropertiesResponse struct {
	Updated []string `json:"updated"`
	Removed []string `json:"removed"`
	Missing []string `json:"missing"`
}

func (h *Handlers) ListCatalogNamespaces(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	items, err := h.Repo.ListNamespaces(r.Context(), projectRID(r))
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to list namespaces")
		return
	}
	var parent []string
	if p := r.URL.Query().Get("parent"); p != "" {
		parent = namespacePath(p)
	}
	out := make([][]string, 0, len(items))
	for _, ns := range items {
		path := namespacePath(ns.Name)
		if len(parent) > 0 && !hasParent(path, parent) {
			continue
		}
		out = append(out, path)
	}
	writeJSON(w, http.StatusOK, restNamespaceListResponse{Namespaces: out})
}

func (h *Handlers) CreateCatalogNamespace(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	store, ok := h.Repo.(catalogNamespaceStore)
	if !ok {
		writeJSONErr(w, http.StatusInternalServerError, "namespace catalog store unavailable")
		return
	}
	var body restCreateNamespaceRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(body.Namespace) == 0 {
		writeJSONErr(w, http.StatusBadRequest, "namespace must contain at least one segment")
		return
	}
	props, _ := json.Marshal(body.Properties)
	_, err := store.CreateNamespace(r.Context(), &models.CreateNamespaceRequest{
		ProjectRID: projectRID(r),
		Name:       strings.Join(body.Namespace, "."),
		Properties: props,
	}, caller.Sub)
	if err != nil {
		writeJSONErr(w, statusFromErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, restNamespaceResponse{Namespace: body.Namespace, Properties: body.Properties})
}

func (h *Handlers) LoadCatalogNamespace(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ns, ok := h.catalogNamespaceFromRoute(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, restNamespaceResponse{Namespace: namespacePath(ns.Name), Properties: namespaceProperties(ns)})
}

func (h *Handlers) DropCatalogNamespace(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ns, ok := h.catalogNamespaceFromRoute(w, r)
	if !ok {
		return
	}
	store := h.Repo.(catalogNamespaceStore)
	deleted, err := store.DeleteNamespace(r.Context(), ns.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !deleted {
		writeJSONErr(w, http.StatusNotFound, "namespace not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) GetNamespaceProperties(w http.ResponseWriter, r *http.Request) {
	h.LoadCatalogNamespace(w, r)
}

func (h *Handlers) UpdateNamespacePropertiesREST(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	ns, ok := h.catalogNamespaceFromRoute(w, r)
	if !ok {
		return
	}
	var body restUpdatePropertiesRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	props := namespaceProperties(ns)
	removed := make([]string, 0, len(body.Removals))
	missing := make([]string, 0, len(body.Removals))
	for _, key := range body.Removals {
		if _, exists := props[key]; exists {
			delete(props, key)
			removed = append(removed, key)
		} else {
			missing = append(missing, key)
		}
	}
	updated := make([]string, 0, len(body.Updates))
	for key, value := range body.Updates {
		props[key] = value
		updated = append(updated, key)
	}
	sort.Strings(updated)
	encoded, _ := json.Marshal(props)
	store := h.Repo.(catalogNamespaceStore)
	if _, err := store.UpdateNamespaceProperties(r.Context(), ns.ID, encoded); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, restUpdatePropertiesResponse{Updated: updated, Removed: removed, Missing: missing})
}

func (h *Handlers) catalogNamespaceFromRoute(w http.ResponseWriter, r *http.Request) (*models.IcebergNamespace, bool) {
	store, ok := h.Repo.(catalogNamespaceStore)
	if !ok {
		writeJSONErr(w, http.StatusInternalServerError, "namespace catalog store unavailable")
		return nil, false
	}
	ns, err := store.FetchNamespaceByName(r.Context(), projectRID(r), namespacePath(chi.URLParam(r, "namespace")))
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if ns == nil {
		writeJSONErr(w, http.StatusNotFound, "namespace not found")
		return nil, false
	}
	return ns, true
}

func (h *Handlers) TableExists(w http.ResponseWriter, r *http.Request) {
	_, t, ok := h.tableFromRoute(w, r)
	if !ok {
		return
	}
	if t == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type alterSchemaRequest struct {
	Updates []map[string]any `json:"updates"`
}

type alterSchemaResponse struct {
	SchemaID int64           `json:"schema_id"`
	Schema   json.RawMessage `json:"schema"`
}

func (h *Handlers) AlterSchema(w http.ResponseWriter, r *http.Request) {
	_, current, ok := h.tableFromRoute(w, r)
	if !ok {
		return
	}
	store, ok := h.Repo.(alterSchemaStore)
	if !ok {
		writeJSONErr(w, http.StatusInternalServerError, "alter-schema store unavailable")
		return
	}
	var body alterSchemaRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	next, schemaID, err := applySchemaUpdates(current.SchemaJSON, body.Updates)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := store.UpdateTableSchema(r.Context(), current.ID, next); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, alterSchemaResponse{SchemaID: schemaID, Schema: next})
}

func (h *Handlers) ListIcebergTables(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	store, ok := h.Repo.(adminStore)
	if !ok {
		writeJSONErr(w, http.StatusInternalServerError, "admin store unavailable")
		return
	}
	items, err := store.ListAdminTables(r.Context(), models.ListIcebergTablesQuery{
		ProjectRID: r.URL.Query().Get("project_rid"),
		Namespace:  r.URL.Query().Get("namespace"),
		Name:       r.URL.Query().Get("name"),
		Sort:       r.URL.Query().Get("sort"),
	})
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.IcebergTableListResponse{Tables: items})
}

func (h *Handlers) GetIcebergTableDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	t, ok := h.adminTableFromRoute(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, models.IcebergTableDetail{
		Summary:                 summarizeTable(t, nil),
		Schema:                  t.SchemaJSON,
		Properties:              t.Properties,
		PartitionSpec:           t.PartitionSpec,
		SortOrder:               t.SortOrder,
		CurrentMetadataLocation: t.CurrentMetadataLocation,
		CurrentSnapshotID:       t.CurrentSnapshotID,
		LastSequenceNumber:      t.LastSequenceNumber,
	})
}

func (h *Handlers) ListIcebergTableSnapshots(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	t, ok := h.adminTableFromRoute(w, r)
	if !ok {
		return
	}
	snaps, err := h.Repo.ListSnapshots(r.Context(), t.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	entries := make([]models.SnapshotEntry, 0, len(snaps))
	for _, s := range snaps {
		entries = append(entries, models.SnapshotEntry{SnapshotID: s.SnapshotID, ParentSnapshotID: s.ParentSnapshotID, Operation: s.Operation, Timestamp: time.UnixMilli(s.TimestampMS).UTC(), SequenceNumber: s.SequenceNumber, ManifestList: s.ManifestListLocation, SchemaID: s.SchemaID, Summary: s.Summary})
	}
	writeJSON(w, http.StatusOK, models.SnapshotListResponse{Snapshots: entries})
}

func (h *Handlers) GetIcebergTableMetadata(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	t, ok := h.adminTableFromRoute(w, r)
	if !ok {
		return
	}
	files, err := h.Repo.ListMetadataFiles(r.Context(), t.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	metadata, err := h.tableMetadata(r.Context(), t)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	history := make([]models.MetadataHistoryEntry, 0, len(files))
	for _, f := range files {
		history = append(history, models.MetadataHistoryEntry{Version: f.Version, Path: f.Path, CreatedAt: f.CreatedAt})
	}
	location := fmt.Sprintf("%s/metadata/v1.metadata.json", t.Location)
	if t.CurrentMetadataLocation != nil {
		location = *t.CurrentMetadataLocation
	}
	writeJSON(w, http.StatusOK, models.MetadataResponse{Metadata: metadata, MetadataLocation: location, History: history})
}

func (h *Handlers) ListIcebergTableBranches(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	t, ok := h.adminTableFromRoute(w, r)
	if !ok {
		return
	}
	refs, err := h.Repo.ListRefs(r.Context(), t.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	branches := make([]models.BranchEntry, 0, len(refs))
	for _, ref := range refs {
		branches = append(branches, models.BranchEntry{Name: ref.Name, Kind: ref.Kind, SnapshotID: ref.SnapshotID})
	}
	writeJSON(w, http.StatusOK, models.BranchListResponse{Branches: branches})
}

func (h *Handlers) adminTableFromRoute(w http.ResponseWriter, r *http.Request) (*models.IcebergTable, bool) {
	store, ok := h.Repo.(adminStore)
	if !ok {
		writeJSONErr(w, http.StatusInternalServerError, "admin store unavailable")
		return nil, false
	}
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return nil, false
	}
	t, err := store.GetTableByRID(r.Context(), "ri.foundry.main.iceberg-table."+id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if t == nil {
		writeJSONErr(w, http.StatusNotFound, "table not found")
		return nil, false
	}
	return t, true
}

func namespaceProperties(ns *models.IcebergNamespace) map[string]string {
	props := map[string]string{}
	if len(ns.Properties) == 0 {
		return props
	}
	var raw map[string]any
	if json.Unmarshal(ns.Properties, &raw) == nil {
		for k, v := range raw {
			props[k] = fmt.Sprint(v)
		}
	}
	return props
}

func hasParent(path, parent []string) bool {
	if len(path) != len(parent)+1 {
		return false
	}
	for i := range parent {
		if path[i] != parent[i] {
			return false
		}
	}
	return true
}

func applySchemaUpdates(current json.RawMessage, updates []map[string]any) (json.RawMessage, int64, error) {
	var schema map[string]any
	if err := json.Unmarshal(current, &schema); err != nil || schema == nil {
		return nil, 0, fmt.Errorf("invalid current schema")
	}
	fields, _ := schema["fields"].([]any)
	nextID := int64(0)
	for _, f := range fields {
		if m, ok := f.(map[string]any); ok {
			if id, ok := numberAsInt64(m["id"]); ok && id > nextID {
				nextID = id
			}
		}
	}
	for _, upd := range updates {
		action, _ := upd["action"].(string)
		switch action {
		case "add-column":
			name, _ := upd["name"].(string)
			typ := upd["type"]
			if name == "" || typ == nil {
				return nil, 0, fmt.Errorf("add-column requires name and type")
			}
			nextID++
			fields = append(fields, map[string]any{"id": nextID, "name": name, "type": typ, "required": false})
		case "drop-column":
			name, _ := upd["name"].(string)
			kept := fields[:0]
			for _, f := range fields {
				if m, ok := f.(map[string]any); ok && m["name"] == name {
					continue
				}
				kept = append(kept, f)
			}
			fields = kept
		case "rename-column":
			name, _ := upd["name"].(string)
			newName, _ := upd["new-name"].(string)
			if newName == "" {
				newName, _ = upd["new_name"].(string)
			}
			for _, f := range fields {
				if m, ok := f.(map[string]any); ok && m["name"] == name {
					m["name"] = newName
				}
			}
		case "update-column":
			name, _ := upd["name"].(string)
			for _, f := range fields {
				if m, ok := f.(map[string]any); ok && m["name"] == name {
					if typ, ok := upd["type"]; ok {
						m["type"] = typ
					}
					if req, ok := upd["required"]; ok {
						m["required"] = req
					}
				}
			}
		default:
			return nil, 0, fmt.Errorf("unsupported schema update action %q", action)
		}
	}
	schemaID, _ := numberAsInt64(schema["schema-id"])
	schemaID++
	schema["schema-id"] = schemaID
	schema["fields"] = fields
	out, err := json.Marshal(schema)
	return out, schemaID, err
}

func numberAsInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	default:
		return 0, false
	}
}

func summarizeTable(t *models.IcebergTable, lastSnapshotAt *time.Time) models.IcebergTableSummary {
	return models.IcebergTableSummary{ID: t.ID, RID: t.RID, Namespace: t.Namespace, Name: t.Name, FormatVersion: t.FormatVersion, Location: t.Location, Markings: t.Markings, LastSnapshotAt: lastSnapshotAt, CreatedAt: t.CreatedAt}
}
