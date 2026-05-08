package workspace

// resource_resolve.go ports
// services/tenancy-organizations-service/src/handlers/resource_resolve.rs.
//
// Cross-resource label resolver. `POST /api/v1/workspace/resources/resolve`
// accepts a batch of `(resource_kind, resource_id)` pairs and returns a
// single map of human-friendly labels. The frontend uses this so the
// workspace surface (favorites, recents, trash, search results) doesn't
// fall back to `kind · id-prefix` placeholders for every non-ontology
// row.
//
// In Phase 1 the service can resolve resources whose authority lives in
// databases it has direct access to:
//
//   - ontology_project → ontology_projects.display_name (or slug fallback)
//   - ontology_folder  → ontology_project_folders.name
//
// For other kinds (datasets, pipelines, notebooks, …) the response
// reports `resolved: false` so the caller keeps using its placeholder.
// Adding HTTP clients to fan out to those services is intentionally
// deferred — the contract is shaped to absorb that later without
// breaking callers.

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// MaxResolveBatch caps the number of items per request so a misbehaving
// caller can't issue an unbounded batch. Mirrors the Rust MAX_BATCH.
const MaxResolveBatch = 200

// ResolveRequestEntry is one (kind, id) pair in a resolve request.
type ResolveRequestEntry struct {
	ResourceKind string    `json:"resource_kind"`
	ResourceID   uuid.UUID `json:"resource_id"`
}

// ResolveRequest is the body of POST /api/v1/workspace/resources/resolve.
type ResolveRequest struct {
	Items []ResolveRequestEntry `json:"items"`
}

// ResolvedLabel is one entry in the resolve response. `Label` and
// `Description` are pointer-to-string so they serialize as JSON `null`
// for unsupported kinds and unknown ids — the wire shape Rust emits.
type ResolvedLabel struct {
	ResourceKind string    `json:"resource_kind"`
	ResourceID   uuid.UUID `json:"resource_id"`
	Resolved     bool      `json:"resolved"`
	Label        *string   `json:"label"`
	Description  *string   `json:"description"`
}

// ResolveResponse pins the {data: [...]} envelope used across the
// workspace surface (matches Rust impl).
type ResolveResponse struct {
	Data []ResolvedLabel `json:"data"`
}

// labelRow holds the (label, description) pair pulled from Postgres for
// each resolvable kind. `description` is nullable in Rust (Option<String>)
// even though the column is NOT NULL DEFAULT '' — the Go port stores it
// as *string for the same reason.
type labelRow struct {
	label       string
	description *string
}

// ResolveProjectLabels fetches (label, description) for the given
// ontology_project ids in a single query. Missing ids are simply absent
// from the returned map. The label uses
// COALESCE(NULLIF(display_name, ''), slug) so a blank display_name
// falls back to the slug — byte-exact with Rust SQL.
func (r *Repo) ResolveProjectLabels(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]labelRow, error) {
	out := make(map[uuid.UUID]labelRow, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT id, COALESCE(NULLIF(display_name, ''), slug) AS label, description
		 FROM ontology_projects
		 WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id    uuid.UUID
			label string
			desc  *string
		)
		if err := rows.Scan(&id, &label, &desc); err != nil {
			return nil, err
		}
		out[id] = labelRow{label: label, description: desc}
	}
	return out, rows.Err()
}

// ResolveFolderLabels fetches (label, description) for the given
// ontology_project_folder ids in a single query.
func (r *Repo) ResolveFolderLabels(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]labelRow, error) {
	out := make(map[uuid.UUID]labelRow, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT id, name, description
		 FROM ontology_project_folders
		 WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id    uuid.UUID
			label string
			desc  *string
		)
		if err := rows.Scan(&id, &label, &desc); err != nil {
			return nil, err
		}
		out[id] = labelRow{label: label, description: desc}
	}
	return out, rows.Err()
}

// ResolveResources handles POST /api/v1/workspace/resources/resolve.
//
// Behaviour mirrors the Rust handler in resource_resolve.rs:
//   - Empty `items` → 200 with `{"data": []}`.
//   - More than MaxResolveBatch items → 400.
//   - Items are bucketed by kind and resolved with one query per
//     resolvable table (projects, folders).
//   - Unsupported kinds and unknown ids emit `resolved: false` with
//     `label`/`description` set to JSON null, preserving request order.
func (h *Handlers) ResolveResources(w http.ResponseWriter, r *http.Request) {
	if _, ok := authmw.FromContext(r.Context()); !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var body ResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if len(body.Items) == 0 {
		writeJSON(w, http.StatusOK, ResolveResponse{Data: []ResolvedLabel{}})
		return
	}
	if len(body.Items) > MaxResolveBatch {
		writeJSONErr(w, http.StatusBadRequest, fmt.Sprintf("at most %d items per request", MaxResolveBatch))
		return
	}

	// Bucket entries by kind so we can fire one query per database table
	// instead of one per item. Preserve the original (kind, id) ordering
	// so the response mirrors the request — unsupported entries are
	// emitted as `resolved: false` below.
	type orderEntry struct {
		kind string
		id   uuid.UUID
	}
	order := make([]orderEntry, 0, len(body.Items))
	var (
		projectIDs []uuid.UUID
		folderIDs  []uuid.UUID
	)
	for _, e := range body.Items {
		order = append(order, orderEntry{kind: e.ResourceKind, id: e.ResourceID})
		// Use ParseResourceKind so legacy aliases (`project`, `folder`)
		// route to the same buckets as their canonical spellings.
		kind, err := ParseResourceKind(e.ResourceKind)
		if err != nil {
			continue
		}
		switch kind {
		case ResourceOntologyProject:
			projectIDs = append(projectIDs, e.ResourceID)
		case ResourceOntologyFolder:
			folderIDs = append(folderIDs, e.ResourceID)
		}
	}

	projectLabels, err := h.Repo.ResolveProjectLabels(r.Context(), projectIDs)
	if err != nil {
		slog.Error("resolve.projects", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("resolve.projects: %s", err))
		return
	}
	folderLabels, err := h.Repo.ResolveFolderLabels(r.Context(), folderIDs)
	if err != nil {
		slog.Error("resolve.folders", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("resolve.folders: %s", err))
		return
	}

	data := make([]ResolvedLabel, 0, len(order))
	for _, entry := range order {
		out := ResolvedLabel{
			ResourceKind: entry.kind,
			ResourceID:   entry.id,
		}
		kind, err := ParseResourceKind(entry.kind)
		if err == nil {
			var (
				row labelRow
				hit bool
			)
			switch kind {
			case ResourceOntologyProject:
				row, hit = projectLabels[entry.id]
			case ResourceOntologyFolder:
				row, hit = folderLabels[entry.id]
			}
			if hit {
				label := row.label
				out.Resolved = true
				out.Label = &label
				out.Description = row.description
			}
		}
		data = append(data, out)
	}

	writeJSON(w, http.StatusOK, ResolveResponse{Data: data})
}
