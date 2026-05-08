// Package search ports `libs/ontology-kernel/src/handlers/search.rs`
// 1:1: the 9 endpoints that drive the ontology search surface
// (free-text + object-instance full-text + the `/graph` builder)
// and the Quiver visual-function CRUD that lives alongside them
// because it shares the time-series spec helper.
//
// Endpoints (matched against `lib.rs::build_router`):
//
//   - POST /ontology/search                 → SearchOntology
//   - GET  /ontology/search                 → SearchObjectsFulltext
//   - GET  /ontology/graph                  → GetGraph
//   - GET  /ontology/quiver                 → ListQuiverVisualFunctions
//   - POST /ontology/quiver                 → CreateQuiverVisualFunction
//   - GET  /ontology/quiver/{id}            → GetQuiverVisualFunction
//   - PATCH /ontology/quiver/{id}           → UpdateQuiverVisualFunction
//   - DELETE /ontology/quiver/{id}          → DeleteQuiverVisualFunction
//   - POST /ontology/quiver/vega-spec       → GetQuiverVegaSpec
//
// All handlers thread through `authmw.FromContext(r.Context())`;
// missing claims surface 401. Every error path uses the package's
// `writeJSON` + `errBody` helpers so the wire shape matches the
// other ontology-kernel handlers byte-for-byte.

package search

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	domsearch "github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain/search"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// Mount registers every search-handler endpoint on the chi router
// under the same path / verb shape as `lib.rs::build_router`.
func Mount(r chi.Router, state *ontologykernel.AppState, backend storage.SearchBackend) {
	r.Post("/ontology/search", SearchOntology(state, backend))
	r.Get("/ontology/search", SearchObjectsFulltext(state, backend))
	r.Get("/ontology/graph", GetGraph(state, backend))
	r.Get("/ontology/quiver", ListQuiverVisualFunctions(state))
	r.Post("/ontology/quiver", CreateQuiverVisualFunction(state))
	r.Get("/ontology/quiver/{id}", GetQuiverVisualFunction(state))
	r.Patch("/ontology/quiver/{id}", UpdateQuiverVisualFunction(state))
	r.Delete("/ontology/quiver/{id}", DeleteQuiverVisualFunction(state))
	r.Post("/ontology/quiver/vega-spec", GetQuiverVegaSpec(state))
}

// ── HTTP plumbing helpers ────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func errBody(msg string) map[string]string { return map[string]string{"error": msg} }

func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, errBody(msg))
}
func notFound(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusNotFound, errBody(msg))
}
func forbidden(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusForbidden, errBody(msg))
}
func internalError(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusInternalServerError, errBody(msg))
}
func unauthorized(w http.ResponseWriter) {
	writeJSON(w, http.StatusUnauthorized, errBody("missing claims"))
}

func pathUUID(r *http.Request, key string) (uuid.UUID, error) {
	raw := chi.URLParam(r, key)
	if raw == "" {
		return uuid.Nil, errors.New("missing path parameter " + key)
	}
	return uuid.Parse(strings.TrimSpace(raw))
}

// ── Endpoints ────────────────────────────────────────────────────────────

// SearchOntology mirrors `pub async fn search_ontology` (POST). Body
// is a [models.SearchRequest]; an empty `query` rejects with 400.
func SearchOntology(state *ontologykernel.AppState, _ storage.SearchBackend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		var body models.SearchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		if strings.TrimSpace(body.Query) == "" {
			badRequest(w, "query is required")
			return
		}

		loader := buildSearchDocumentsLoader(state)
		results, err := domsearch.SearchOntology(r.Context(), domsearch.OntologySearchDeps{
			DB:                          state.DB,
			HTTPClient:                  state.HTTPClient,
			ConfiguredEmbeddingProvider: state.SearchEmbeddingProvider,
		}, loader, claims, body)
		if err != nil {
			internalError(w, "ontology search failed")
			return
		}
		writeJSON(w, http.StatusOK, models.SearchResponse{
			Query: body.Query,
			Total: len(results),
			Data:  results,
		})
	}
}

// SearchObjectsFulltext mirrors `pub async fn search_objects_fulltext`
// (GET). Query parameters: `q` (required), `type` (optional UUID),
// `marking` (optional CSV), `limit` (optional, clamped to [1, 200]).
func SearchObjectsFulltext(state *ontologykernel.AppState, backend storage.SearchBackend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			badRequest(w, "q is required")
			return
		}

		var objectTypeID *uuid.UUID
		if raw := r.URL.Query().Get("type"); raw != "" {
			if id, err := uuid.Parse(raw); err == nil {
				objectTypeID = &id
			}
		}
		var markings *[]string
		if raw := r.URL.Query().Get("marking"); raw != "" {
			parts := []string{}
			for _, p := range strings.Split(raw, ",") {
				if p = strings.TrimSpace(p); p != "" {
					parts = append(parts, p)
				}
			}
			if len(parts) > 0 {
				markings = &parts
			}
		}
		limit := int64(50)
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
				limit = v
			}
		}

		if backend == nil {
			internalError(w, "ontology full-text search failed")
			return
		}
		hits, err := domsearch.SearchObjects(r.Context(), backend, claims, domsearch.ObjectFulltextQuery{
			Query:        q,
			ObjectTypeID: objectTypeID,
			Markings:     markings,
			Limit:        limit,
		})
		if err != nil {
			internalError(w, "ontology full-text search failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"query": q,
			"total": len(hits),
			"data":  hits,
		})
	}
}

// GetGraph mirrors `pub async fn get_graph`. Forbidden / not-found
// errors are mapped to 403/404 by sniffing the error string the
// domain layer surfaces (mirrors the Rust `error.contains("forbidden")`
// dispatch — kept verbatim so the wire status codes match).
func GetGraph(state *ontologykernel.AppState, backend storage.SearchBackend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		query := parseGraphQuery(r)
		loader := buildGraphObjectLoader(state, backend)

		response, err := domain.BuildGraph(r.Context(), state.DB, state.Stores.Links, claims, loader, query)
		if err != nil {
			msg := err.Error()
			switch {
			case strings.Contains(msg, "forbidden"):
				forbidden(w, msg)
			case strings.Contains(msg, "not found") || strings.Contains(msg, "not be found"):
				notFound(w, msg)
			default:
				badRequest(w, msg)
			}
			return
		}
		writeJSON(w, http.StatusOK, response)
	}
}

// ListQuiverVisualFunctions mirrors `pub async fn list_quiver_visual_functions`.
// The SQL is preserved byte-for-byte (with `claims.sub` and the
// `include_shared` flag bound as `$1`/`$2`).
func ListQuiverVisualFunctions(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}

		page := int64(1)
		perPage := int64(20)
		searchValue := ""
		includeShared := true
		if raw := r.URL.Query().Get("page"); raw != "" {
			if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 1 {
				page = v
			}
		}
		if raw := r.URL.Query().Get("per_page"); raw != "" {
			if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
				perPage = v
			}
		}
		if raw := r.URL.Query().Get("search"); raw != "" {
			searchValue = raw
		}
		if raw := r.URL.Query().Get("include_shared"); raw != "" {
			if v, err := strconv.ParseBool(raw); err == nil {
				includeShared = v
			}
		}
		if perPage < 1 {
			perPage = 1
		}
		if perPage > 100 {
			perPage = 100
		}
		offset := (page - 1) * perPage
		searchPattern := "%" + searchValue + "%"

		var total int64
		err := state.DB.QueryRow(r.Context(),
			`SELECT COUNT(*)
               FROM ontology_quiver_visual_functions
               WHERE (owner_id = $1 OR ($2 AND shared = TRUE))
                 AND ($3 = '%%' OR name ILIKE $3 OR description ILIKE $3)`,
			claims.Sub, includeShared, searchPattern,
		).Scan(&total)
		if err != nil {
			internalError(w, "failed to list quiver visual functions")
			return
		}

		rows, err := state.DB.Query(r.Context(),
			`SELECT id, name, description, primary_type_id, secondary_type_id, join_field,
                      secondary_join_field, date_field, metric_field, group_field, selected_group,
                      chart_kind, shared, vega_spec, owner_id, created_at, updated_at
               FROM ontology_quiver_visual_functions
               WHERE (owner_id = $1 OR ($2 AND shared = TRUE))
                 AND ($3 = '%%' OR name ILIKE $3 OR description ILIKE $3)
               ORDER BY updated_at DESC
               LIMIT $4 OFFSET $5`,
			claims.Sub, includeShared, searchPattern, perPage, offset,
		)
		if err != nil {
			internalError(w, "failed to list quiver visual functions")
			return
		}
		defer rows.Close()
		records := []models.QuiverVisualFunction{}
		for rows.Next() {
			rec, err := scanQuiverRow(rows)
			if err != nil {
				internalError(w, "failed to list quiver visual functions")
				return
			}
			records = append(records, rec)
		}
		if err := rows.Err(); err != nil {
			internalError(w, "failed to list quiver visual functions")
			return
		}
		writeJSON(w, http.StatusOK, models.ListQuiverVisualFunctionsResponse{
			Data:    records,
			Total:   total,
			Page:    page,
			PerPage: perPage,
		})
	}
}

// CreateQuiverVisualFunction mirrors `pub async fn create_quiver_visual_function`.
func CreateQuiverVisualFunction(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		var body models.CreateQuiverVisualFunctionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		draft := body.IntoDraft()
		if err := validateVisualFunctionDraft(draft); err != nil {
			badRequest(w, err.Error())
			return
		}
		vegaSpec, err := domain.BuildQuiverVegaSpec(draft)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		id, err := uuid.NewV7()
		if err != nil {
			internalError(w, "failed to create quiver visual function")
			return
		}
		row := state.DB.QueryRow(r.Context(), `INSERT INTO ontology_quiver_visual_functions (
                   id, name, description, primary_type_id, secondary_type_id, join_field,
                   secondary_join_field, date_field, metric_field, group_field, selected_group,
                   chart_kind, shared, vega_spec, owner_id
               )
               VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14::jsonb, $15)
               RETURNING id, name, description, primary_type_id, secondary_type_id, join_field,
                         secondary_join_field, date_field, metric_field, group_field, selected_group,
                         chart_kind, shared, vega_spec, owner_id, created_at, updated_at`,
			id, draft.Name, draft.Description, draft.PrimaryTypeID, draft.SecondaryTypeID,
			draft.JoinField, draft.SecondaryJoinField, draft.DateField,
			draft.MetricField, draft.GroupField, draft.SelectedGroup,
			draft.ChartKind, draft.Shared, vegaSpec, claims.Sub,
		)
		created, err := scanQuiverRow(row)
		if err != nil {
			internalError(w, "failed to create quiver visual function")
			return
		}
		writeJSON(w, http.StatusCreated, created)
	}
}

// GetQuiverVisualFunction mirrors `pub async fn get_quiver_visual_function`.
// Sharing rule: owner OR shared=true; otherwise 403.
func GetQuiverVisualFunction(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		record, err := loadQuiverVisualFunction(r.Context(), state, id)
		if err != nil {
			internalError(w, "failed to load quiver visual function")
			return
		}
		if record == nil {
			notFound(w, "quiver visual function not found")
			return
		}
		if record.OwnerID != claims.Sub && !record.Shared {
			forbidden(w, "you do not have access to this quiver visual function")
			return
		}
		writeJSON(w, http.StatusOK, record)
	}
}

// UpdateQuiverVisualFunction mirrors `pub async fn update_quiver_visual_function`.
// Only the owner can update. Body fields are optional; absent fields
// keep the current value.
func UpdateQuiverVisualFunction(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		current, err := loadQuiverVisualFunction(r.Context(), state, id)
		if err != nil {
			internalError(w, "failed to load quiver visual function")
			return
		}
		if current == nil {
			notFound(w, "quiver visual function not found")
			return
		}
		if current.OwnerID != claims.Sub {
			forbidden(w, "only the owner can update this quiver visual function")
			return
		}
		var body models.UpdateQuiverVisualFunctionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		draft := applyQuiverUpdate(*current, body)
		if err := validateVisualFunctionDraft(draft); err != nil {
			badRequest(w, err.Error())
			return
		}
		vegaSpec, err := domain.BuildQuiverVegaSpec(draft)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		row := state.DB.QueryRow(r.Context(), `UPDATE ontology_quiver_visual_functions
               SET name = $2,
                   description = $3,
                   primary_type_id = $4,
                   secondary_type_id = $5,
                   join_field = $6,
                   secondary_join_field = $7,
                   date_field = $8,
                   metric_field = $9,
                   group_field = $10,
                   selected_group = $11,
                   chart_kind = $12,
                   shared = $13,
                   vega_spec = $14::jsonb,
                   updated_at = NOW()
               WHERE id = $1
               RETURNING id, name, description, primary_type_id, secondary_type_id, join_field,
                         secondary_join_field, date_field, metric_field, group_field, selected_group,
                         chart_kind, shared, vega_spec, owner_id, created_at, updated_at`,
			id, draft.Name, draft.Description, draft.PrimaryTypeID, draft.SecondaryTypeID,
			draft.JoinField, draft.SecondaryJoinField, draft.DateField,
			draft.MetricField, draft.GroupField, draft.SelectedGroup,
			draft.ChartKind, draft.Shared, vegaSpec,
		)
		updated, err := scanQuiverRow(row)
		if err != nil {
			internalError(w, "failed to update quiver visual function")
			return
		}
		writeJSON(w, http.StatusOK, updated)
	}
}

// DeleteQuiverVisualFunction mirrors `pub async fn delete_quiver_visual_function`.
// Only the owner can delete; missing id → 404.
func DeleteQuiverVisualFunction(state *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmw.FromContext(r.Context())
		if !ok {
			unauthorized(w)
			return
		}
		id, err := pathUUID(r, "id")
		if err != nil {
			badRequest(w, "invalid path id")
			return
		}
		record, err := loadQuiverVisualFunction(r.Context(), state, id)
		if err != nil {
			internalError(w, "failed to load quiver visual function")
			return
		}
		if record == nil {
			notFound(w, "quiver visual function not found")
			return
		}
		if record.OwnerID != claims.Sub {
			forbidden(w, "only the owner can delete this quiver visual function")
			return
		}
		tag, err := state.DB.Exec(r.Context(),
			`DELETE FROM ontology_quiver_visual_functions WHERE id = $1`,
			id,
		)
		if err != nil {
			internalError(w, "failed to delete quiver visual function")
			return
		}
		if tag.RowsAffected() == 0 {
			notFound(w, "quiver visual function not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// GetQuiverVegaSpec mirrors `pub async fn get_quiver_vega_spec`. A
// preview-only endpoint that builds the Vega spec from a request
// body without persisting.
func GetQuiverVegaSpec(_ *ontologykernel.AppState) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body models.CreateQuiverVisualFunctionRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			badRequest(w, "invalid request body")
			return
		}
		draft := body.IntoDraft()
		if err := validateVisualFunctionDraft(draft); err != nil {
			badRequest(w, err.Error())
			return
		}
		spec, err := domain.BuildQuiverVegaSpec(draft)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]json.RawMessage{"spec": spec})
	}
}

// ── shared helpers ───────────────────────────────────────────────────────

// validateVisualFunctionDraft mirrors `fn validate_visual_function_draft`.
func validateVisualFunctionDraft(draft models.QuiverVisualFunctionDraft) error {
	if strings.TrimSpace(draft.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(draft.JoinField) == "" {
		return errors.New("join_field is required")
	}
	if strings.TrimSpace(draft.DateField) == "" {
		return errors.New("date_field is required")
	}
	if strings.TrimSpace(draft.MetricField) == "" {
		return errors.New("metric_field is required")
	}
	if strings.TrimSpace(draft.GroupField) == "" {
		return errors.New("group_field is required")
	}
	if _, err := domain.NormalizeChartKind(draft.ChartKind); err != nil {
		return err
	}
	return nil
}

// applyQuiverUpdate mirrors `fn apply_quiver_update` — overlay the
// PATCH body on the current record and produce a fresh draft.
func applyQuiverUpdate(current models.QuiverVisualFunction, update models.UpdateQuiverVisualFunctionRequest) models.QuiverVisualFunctionDraft {
	str := func(p *string, fallback string) string {
		if p == nil {
			return fallback
		}
		return *p
	}
	uuidPtr := func(p *uuid.UUID, fallback uuid.UUID) uuid.UUID {
		if p == nil {
			return fallback
		}
		return *p
	}
	bool_ := func(p *bool, fallback bool) bool {
		if p == nil {
			return fallback
		}
		return *p
	}
	// secondary_type_id is `update.or(current)` — i.e. update wins
	// when present, else keep current. (Rust `update.secondary_type_id.or(current.secondary_type_id)`)
	secondary := current.SecondaryTypeID
	if update.SecondaryTypeID != nil {
		secondary = update.SecondaryTypeID
	}
	// selected_group carries Option<Option<String>> three-way.
	// .None => keep current; Some(None) => clear; Some(Some(v)) => set.
	selected := current.SelectedGroup
	if update.SelectedGroup != nil {
		selected = update.SelectedGroup.Value
	}

	return models.QuiverVisualFunctionDraft{
		Name:               str(update.Name, current.Name),
		Description:        str(update.Description, current.Description),
		PrimaryTypeID:      uuidPtr(update.PrimaryTypeID, current.PrimaryTypeID),
		SecondaryTypeID:    secondary,
		JoinField:          str(update.JoinField, current.JoinField),
		SecondaryJoinField: str(update.SecondaryJoinField, current.SecondaryJoinField),
		DateField:          str(update.DateField, current.DateField),
		MetricField:        str(update.MetricField, current.MetricField),
		GroupField:         str(update.GroupField, current.GroupField),
		SelectedGroup:      selected,
		ChartKind:          str(update.ChartKind, current.ChartKind),
		Shared:             bool_(update.Shared, current.Shared),
	}
}

// loadQuiverVisualFunction mirrors `async fn load_quiver_visual_function`.
func loadQuiverVisualFunction(ctx context.Context, state *ontologykernel.AppState, id uuid.UUID) (*models.QuiverVisualFunction, error) {
	row := state.DB.QueryRow(ctx,
		`SELECT id, name, description, primary_type_id, secondary_type_id, join_field,
                  secondary_join_field, date_field, metric_field, group_field, selected_group,
                  chart_kind, shared, vega_spec, owner_id, created_at, updated_at
           FROM ontology_quiver_visual_functions
           WHERE id = $1`,
		id,
	)
	rec, err := scanQuiverRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// scanQuiverRow reads a row out of either pgx.Row or pgx.Rows.
func scanQuiverRow(row interface{ Scan(...any) error }) (models.QuiverVisualFunction, error) {
	var rec models.QuiverVisualFunction
	if err := row.Scan(
		&rec.ID, &rec.Name, &rec.Description,
		&rec.PrimaryTypeID, &rec.SecondaryTypeID,
		&rec.JoinField, &rec.SecondaryJoinField,
		&rec.DateField, &rec.MetricField, &rec.GroupField,
		&rec.SelectedGroup, &rec.ChartKind, &rec.Shared,
		&rec.VegaSpec, &rec.OwnerID,
		&rec.CreatedAt, &rec.UpdatedAt,
	); err != nil {
		return models.QuiverVisualFunction{}, err
	}
	return rec, nil
}

// parseGraphQuery decodes the GraphQuery from URL parameters
// (root_object_id, root_type_id, depth, limit). Mirrors the Rust
// axum `Query<GraphQuery>` extractor.
func parseGraphQuery(r *http.Request) models.GraphQuery {
	q := r.URL.Query()
	out := models.GraphQuery{}
	if raw := q.Get("root_object_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			out.RootObjectID = &id
		}
	}
	if raw := q.Get("root_type_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			out.RootTypeID = &id
		}
	}
	if raw := q.Get("depth"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			out.Depth = &v
		}
	}
	if raw := q.Get("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			out.Limit = &v
		}
	}
	return out
}

// buildSearchDocumentsLoader wires `domain.BuildSearchDocuments`
// into the [domsearch.DocumentsLoader] signature the orchestrator
// expects.
func buildSearchDocumentsLoader(state *ontologykernel.AppState) domsearch.DocumentsLoader {
	return func(ctx context.Context, claims *authmw.Claims, objectTypeFilter *uuid.UUID, kindFilter *string) ([]domain.SearchDocument, error) {
		return domain.BuildSearchDocuments(ctx, state, claims, objectTypeFilter, kindFilter)
	}
}

// buildGraphObjectLoader wires the read-model lookup for
// [domain.BuildObjectGraph]. Today the SearchBackend half of the
// Rust source is not yet ported on the Go side; the loader falls
// back directly to the ObjectStore — same path the Rust loader
// takes when the search backend has no hit. Returns (nil, nil)
// when the object is unknown.
func buildGraphObjectLoader(state *ontologykernel.AppState, _ storage.SearchBackend) domain.GraphObjectLoader {
	return func(ctx context.Context, claims *authmw.Claims, objectID uuid.UUID) (*domain.ObjectInstance, error) {
		if state == nil || state.Stores.Objects == nil {
			return nil, nil
		}
		tenant := domain.TenantFromClaims(claims)
		obj, err := state.Stores.Objects.Get(ctx, tenant, storage.ObjectId(objectID.String()), storage.Eventual())
		if err != nil {
			return nil, err
		}
		if obj == nil {
			return nil, nil
		}
		var fallbackOrg *uuid.UUID
		if claims != nil {
			fallbackOrg = claims.OrgID
		}
		return domain.ObjectStoreToObjectInstance(*obj, fallbackOrg), nil
	}
}
