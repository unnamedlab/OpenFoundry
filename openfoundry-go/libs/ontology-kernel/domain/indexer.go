// Search-document indexer.
//
// Mirrors `libs/ontology-kernel/src/domain/indexer.rs` 1:1: produces
// the same `SearchDocument` records, in the same order, applying the
// same `kind` / `object_type_filter` filters, with the same body /
// snippet / metadata composition rules. Every SQL query is text-
// identical to the Rust source so EXPLAIN plans match across
// languages.
//
// The Rust impl is consumed by both the search-handler index
// rebuilder and by the storage-insights endpoint that counts
// documents by kind. Both call sites land under the same Go API.
package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// SearchDocument mirrors the Rust struct of the same name.
type SearchDocument struct {
	Kind         string          `json:"kind"`
	ID           uuid.UUID       `json:"id"`
	ObjectTypeID *uuid.UUID      `json:"object_type_id,omitempty"`
	Title        string          `json:"title"`
	Subtitle     *string         `json:"subtitle,omitempty"`
	Snippet      string          `json:"snippet"`
	Body         string          `json:"body"`
	Route        string          `json:"route"`
	Metadata     json.RawMessage `json:"metadata"`
}

const objectSearchPageSize = uint32(512)

// BuildSearchDocuments mirrors `pub async fn build_search_documents`.
//
// Order of emission and field composition match Rust verbatim:
//   1. object_type
//   2. interface
//   3. shared_property_type
//   4. link_type
//   5. action_type
//   6. object_instance
//   7. interface_binding
func BuildSearchDocuments(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	objectTypeFilter *uuid.UUID,
	kindFilter *string,
) ([]SearchDocument, error) {
	objectTypes, err := loadObjectTypesForIndexing(ctx, state.DB)
	if err != nil {
		return nil, fmt.Errorf("failed to load object types: %w", err)
	}
	objectTypeMap := map[uuid.UUID]models.ObjectType{}
	for _, ot := range objectTypes {
		objectTypeMap[ot.ID] = ot
	}

	var documents []SearchDocument

	if kindMatches(kindFilter, "object_type") {
		for _, ot := range objectTypes {
			if objectTypeFilter != nil && *objectTypeFilter != ot.ID {
				continue
			}
			subtitle := ot.Name
			documents = append(documents, SearchDocument{
				Kind:         "object_type",
				ID:           ot.ID,
				ObjectTypeID: uuidPtr(ot.ID),
				Title:        ot.DisplayName,
				Subtitle:     &subtitle,
				Snippet:      ot.Description,
				Body: fmt.Sprintf(
					"%s %s %s %s %s",
					ot.Name, ot.DisplayName, ot.Description,
					strDeref(ot.Icon), strDeref(ot.Color),
				),
				Route: "/ontology/" + ot.ID.String(),
				Metadata: mustJSON(map[string]any{
					"name":                  ot.Name,
					"primary_key_property":  ot.PrimaryKeyProperty,
					"icon":                  ot.Icon,
					"color":                 ot.Color,
				}),
			})
		}
	}

	if kindMatches(kindFilter, "interface") {
		rows, err := loadInterfacesForIndexing(ctx, state.DB, objectTypeFilter)
		if err != nil {
			return nil, fmt.Errorf("failed to load ontology interfaces: %w", err)
		}
		for _, intf := range rows {
			subtitle := intf.Name
			documents = append(documents, SearchDocument{
				Kind:         "interface",
				ID:           intf.ID,
				ObjectTypeID: objectTypeFilter,
				Title:        intf.DisplayName,
				Subtitle:     &subtitle,
				Snippet:      intf.Description,
				Body:         fmt.Sprintf("%s %s %s", intf.Name, intf.DisplayName, intf.Description),
				Route:        "/ontology/graph",
				Metadata:     mustJSON(map[string]any{"name": intf.Name}),
			})
		}
	}

	if kindMatches(kindFilter, "shared_property_type") {
		rows, err := loadSharedPropertyTypesForIndexing(ctx, state.DB, objectTypeFilter)
		if err != nil {
			return nil, fmt.Errorf("failed to load shared property types: %w", err)
		}
		for _, spt := range rows {
			subtitle := spt.Name
			snippet := spt.Description
			if snippet == "" {
				snippet = "Reusable " + spt.PropertyType + " property"
			}
			required := "optional"
			if spt.Required {
				required = "required"
			}
			timeDep := ""
			if spt.TimeDependent {
				timeDep = "time-dependent"
			}
			route := "/ontology/graph"
			if objectTypeFilter != nil {
				route = "/ontology/" + objectTypeFilter.String()
			}
			documents = append(documents, SearchDocument{
				Kind:         "shared_property_type",
				ID:           spt.ID,
				ObjectTypeID: objectTypeFilter,
				Title:        spt.DisplayName,
				Subtitle:     &subtitle,
				Snippet:      snippet,
				Body: fmt.Sprintf(
					"%s %s %s %s %s %s",
					spt.Name, spt.DisplayName, spt.Description,
					spt.PropertyType, required, timeDep,
				),
				Route: route,
				Metadata: mustJSON(map[string]any{
					"name":              spt.Name,
					"property_type":     spt.PropertyType,
					"required":          spt.Required,
					"unique_constraint": spt.UniqueConstraint,
					"time_dependent":    spt.TimeDependent,
				}),
			})
		}
	}

	if kindMatches(kindFilter, "link_type") {
		linkTypes, err := loadLinkTypesForIndexing(ctx, state.DB, objectTypeFilter)
		if err != nil {
			return nil, fmt.Errorf("failed to load link types: %w", err)
		}
		for _, lt := range linkTypes {
			source, sOK := objectTypeMap[lt.SourceTypeID]
			target, tOK := objectTypeMap[lt.TargetTypeID]
			sourceLabel := "unknown"
			sourceName := ""
			if sOK {
				sourceLabel = source.DisplayName
				sourceName = source.Name
			}
			targetLabel := "unknown"
			targetName := ""
			if tOK {
				targetLabel = target.DisplayName
				targetName = target.Name
			}
			subtitle := lt.Name
			documents = append(documents, SearchDocument{
				Kind:         "link_type",
				ID:           lt.ID,
				ObjectTypeID: uuidPtr(lt.SourceTypeID),
				Title:        lt.DisplayName,
				Subtitle:     &subtitle,
				Snippet: fmt.Sprintf(
					"%s -> %s (%s)", sourceLabel, targetLabel, lt.Cardinality,
				),
				Body: fmt.Sprintf(
					"%s %s %s %s %s %s",
					lt.Name, lt.DisplayName, lt.Description,
					sourceName, targetName, lt.Cardinality,
				),
				Route: "/ontology/graph",
				Metadata: mustJSON(map[string]any{
					"source_type_id": lt.SourceTypeID,
					"target_type_id": lt.TargetTypeID,
					"cardinality":    lt.Cardinality,
				}),
			})
		}
	}

	if kindMatches(kindFilter, "action_type") {
		rows, err := loadActionTypesForIndexing(ctx, state.DB, objectTypeFilter)
		if err != nil {
			return nil, fmt.Errorf("failed to load action types: %w", err)
		}
		for _, action := range rows {
			subtitle := action.Name
			permissionKey := strDeref(action.PermissionKey)
			documents = append(documents, SearchDocument{
				Kind:         "action_type",
				ID:           action.ID,
				ObjectTypeID: uuidPtr(action.ObjectTypeID),
				Title:        action.DisplayName,
				Subtitle:     &subtitle,
				Snippet:      action.Description,
				Body: fmt.Sprintf(
					"%s %s %s %s %s",
					action.Name, action.DisplayName, action.Description,
					action.OperationKind, permissionKey,
				),
				Route: "/ontology/" + action.ObjectTypeID.String(),
				Metadata: mustJSON(map[string]any{
					"operation_kind":        action.OperationKind,
					"confirmation_required": action.ConfirmationRequired,
					"permission_key":        action.PermissionKey,
					"authorization_policy":  action.AuthorizationPolicy,
				}),
			})
		}
	}

	if kindMatches(kindFilter, "object_instance") {
		var targetIDs []uuid.UUID
		if objectTypeFilter != nil {
			targetIDs = []uuid.UUID{*objectTypeFilter}
		} else {
			for id := range objectTypeMap {
				targetIDs = append(targetIDs, id)
			}
		}
		objects, err := loadObjectInstancesForSearch(ctx, state, claims, targetIDs)
		if err != nil {
			return nil, err
		}
		for _, obj := range objects {
			ot, ok := objectTypeMap[obj.ObjectTypeID]
			if !ok {
				continue
			}
			propertyTokens, propertyNames := propertyTokensAndNames(obj.Properties)
			subtitle := ot.Name
			documents = append(documents, SearchDocument{
				Kind:         "object_instance",
				ID:           obj.ID,
				ObjectTypeID: uuidPtr(obj.ObjectTypeID),
				Title:        objectTitle(&ot, &obj),
				Subtitle:     &subtitle,
				Snippet:      summarizeObjectProperties(obj.Properties),
				Body: fmt.Sprintf(
					"%s %s %s %s %s",
					ot.Name, ot.DisplayName, propertyTokens, obj.Marking, obj.ID,
				),
				Route: fmt.Sprintf("/ontology/%s#object-%s", obj.ObjectTypeID, obj.ID),
				Metadata: mustJSON(map[string]any{
					"marking":         obj.Marking,
					"organization_id": obj.OrganizationID,
					"properties":      json.RawMessage(obj.Properties),
					"property_names":  propertyNames,
				}),
			})
		}
	}

	if kindMatches(kindFilter, "interface_binding") {
		bindings, err := loadInterfaceBindingsForIndexing(ctx, state.DB, objectTypeFilter)
		if err != nil {
			return nil, fmt.Errorf("failed to load interface bindings: %w", err)
		}
		for _, b := range bindings {
			ot, ok := objectTypeMap[b.ObjectTypeID]
			if !ok {
				continue
			}
			subtitle := b.InterfaceID.String()
			documents = append(documents, SearchDocument{
				Kind:         "interface_binding",
				ID:           b.InterfaceID,
				ObjectTypeID: uuidPtr(b.ObjectTypeID),
				Title:        ot.DisplayName + " interface binding",
				Subtitle:     &subtitle,
				Snippet:      "Interface attached to object type",
				Body:         fmt.Sprintf("%s %s %s", ot.Name, ot.DisplayName, b.InterfaceID),
				Route:        "/ontology/" + b.ObjectTypeID.String(),
				Metadata:     mustJSON(map[string]any{"interface_id": b.InterfaceID}),
			})
		}
	}

	return documents, nil
}

// ── Per-kind SQL loaders (text-identical to the Rust queries). ──────

func loadObjectTypesForIndexing(ctx context.Context, db *pgxpool.Pool) ([]models.ObjectType, error) {
	rows, err := db.Query(ctx, "SELECT * FROM object_types ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ObjectType{}
	for rows.Next() {
		var t models.ObjectType
		if err := rows.Scan(
			&t.ID, &t.Name, &t.DisplayName, &t.Description,
			&t.PrimaryKeyProperty, &t.Icon, &t.Color,
			&t.OwnerID, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func loadInterfacesForIndexing(
	ctx context.Context, db *pgxpool.Pool, objectTypeFilter *uuid.UUID,
) ([]models.OntologyInterface, error) {
	const filtered = `SELECT i.*
                       FROM ontology_interfaces i
                       INNER JOIN object_type_interfaces oti ON oti.interface_id = i.id
                       WHERE oti.object_type_id = $1
                       ORDER BY i.created_at DESC`
	const all = "SELECT * FROM ontology_interfaces ORDER BY created_at DESC"

	var rows pgxRows
	var err error
	if objectTypeFilter != nil {
		rows, err = db.Query(ctx, filtered, *objectTypeFilter)
	} else {
		rows, err = db.Query(ctx, all)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.OntologyInterface{}
	for rows.Next() {
		var intf models.OntologyInterface
		if err := rows.Scan(
			&intf.ID, &intf.Name, &intf.DisplayName, &intf.Description,
			&intf.OwnerID, &intf.CreatedAt, &intf.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, intf)
	}
	return out, rows.Err()
}

func loadSharedPropertyTypesForIndexing(
	ctx context.Context, db *pgxpool.Pool, objectTypeFilter *uuid.UUID,
) ([]models.SharedPropertyType, error) {
	const filtered = `SELECT spt.id, spt.name, spt.display_name, spt.description, spt.property_type,
                              spt.required, spt.unique_constraint, spt.time_dependent, spt.default_value,
                              spt.validation_rules, spt.owner_id, spt.created_at, spt.updated_at
                       FROM shared_property_types spt
                       INNER JOIN object_type_shared_property_types otsp
                           ON otsp.shared_property_type_id = spt.id
                       WHERE otsp.object_type_id = $1
                       ORDER BY otsp.created_at ASC, spt.created_at DESC`
	const all = `SELECT id, name, display_name, description, property_type, required,
                        unique_constraint, time_dependent, default_value, validation_rules,
                        owner_id, created_at, updated_at
                 FROM shared_property_types
                 ORDER BY created_at DESC`

	var rows pgxRows
	var err error
	if objectTypeFilter != nil {
		rows, err = db.Query(ctx, filtered, *objectTypeFilter)
	} else {
		rows, err = db.Query(ctx, all)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.SharedPropertyType{}
	for rows.Next() {
		var s models.SharedPropertyType
		if err := rows.Scan(
			&s.ID, &s.Name, &s.DisplayName, &s.Description, &s.PropertyType,
			&s.Required, &s.UniqueConstraint, &s.TimeDependent, &s.DefaultValue,
			&s.ValidationRules, &s.OwnerID, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func loadLinkTypesForIndexing(
	ctx context.Context, db *pgxpool.Pool, objectTypeFilter *uuid.UUID,
) ([]models.LinkType, error) {
	const filtered = `SELECT * FROM link_types
                       WHERE source_type_id = $1 OR target_type_id = $1
                       ORDER BY created_at DESC`
	const all = "SELECT * FROM link_types ORDER BY created_at DESC"

	var rows pgxRows
	var err error
	if objectTypeFilter != nil {
		rows, err = db.Query(ctx, filtered, *objectTypeFilter)
	} else {
		rows, err = db.Query(ctx, all)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.LinkType{}
	for rows.Next() {
		var lt models.LinkType
		if err := rows.Scan(
			&lt.ID, &lt.Name, &lt.DisplayName, &lt.Description,
			&lt.SourceTypeID, &lt.TargetTypeID, &lt.Cardinality,
			&lt.OwnerID, &lt.CreatedAt, &lt.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, lt)
	}
	return out, rows.Err()
}

func loadActionTypesForIndexing(
	ctx context.Context, db *pgxpool.Pool, objectTypeFilter *uuid.UUID,
) ([]models.ActionTypeRow, error) {
	const cols = `id, name, display_name, description, object_type_id, operation_kind, input_schema,
                  form_schema, config, confirmation_required, permission_key, authorization_policy,
                  owner_id, created_at, updated_at`
	filtered := "SELECT " + cols + " FROM action_types WHERE object_type_id = $1 ORDER BY created_at DESC"
	all := "SELECT " + cols + " FROM action_types ORDER BY created_at DESC"

	var rows pgxRows
	var err error
	if objectTypeFilter != nil {
		rows, err = db.Query(ctx, filtered, *objectTypeFilter)
	} else {
		rows, err = db.Query(ctx, all)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.ActionTypeRow{}
	for rows.Next() {
		var a models.ActionTypeRow
		if err := rows.Scan(
			&a.ID, &a.Name, &a.DisplayName, &a.Description, &a.ObjectTypeID,
			&a.OperationKind, &a.InputSchema, &a.FormSchema, &a.Config,
			&a.ConfirmationRequired, &a.PermissionKey, &a.AuthorizationPolicy,
			&a.OwnerID, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func loadInterfaceBindingsForIndexing(
	ctx context.Context, db *pgxpool.Pool, objectTypeFilter *uuid.UUID,
) ([]models.ObjectTypeInterfaceBinding, error) {
	const filtered = `SELECT object_type_id, interface_id, created_at
                       FROM object_type_interfaces WHERE object_type_id = $1`
	const all = `SELECT object_type_id, interface_id, created_at FROM object_type_interfaces`

	var rows pgxRows
	var err error
	if objectTypeFilter != nil {
		rows, err = db.Query(ctx, filtered, *objectTypeFilter)
	} else {
		rows, err = db.Query(ctx, all)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []models.ObjectTypeInterfaceBinding{}
	for rows.Next() {
		var b models.ObjectTypeInterfaceBinding
		if err := rows.Scan(&b.ObjectTypeID, &b.InterfaceID, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ── Object instance enumeration for the search index ──────────────────

func loadObjectInstancesForSearch(
	ctx context.Context,
	state *ontologykernel.AppState,
	claims *authmw.Claims,
	objectTypeIDs []uuid.UUID,
) ([]ObjectInstance, error) {
	tenant := TenantFromClaims(claims)
	var objects []ObjectInstance

	for _, otID := range objectTypeIDs {
		var token *string
		for {
			page, err := state.Stores.Objects.ListByType(
				ctx, tenant, storage.TypeId(otID.String()),
				storage.Page{Size: objectSearchPageSize, Token: token},
				storage.Eventual(),
			)
			if err != nil {
				return nil, fmt.Errorf("failed to load object instances from object store: %w", err)
			}
			for _, item := range page.Items {
				inst := ObjectStoreToObjectInstance(item, claims.OrgID)
				if inst == nil {
					continue
				}
				if EnsureObjectAccess(claims, inst) != nil {
					continue
				}
				objects = append(objects, *inst)
			}
			if page.NextToken == nil {
				break
			}
			token = page.NextToken
		}
	}
	return objects, nil
}

// ── Helpers (1:1 with the private Rust functions) ────────────────────

func kindMatches(kindFilter *string, candidate string) bool {
	if kindFilter == nil {
		return true
	}
	trimmed := *kindFilter
	for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\t') {
		trimmed = trimmed[1:]
	}
	for len(trimmed) > 0 && (trimmed[len(trimmed)-1] == ' ' || trimmed[len(trimmed)-1] == '\t') {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if trimmed == "" {
		return true
	}
	return trimmed == candidate
}

func compactJSON(value json.RawMessage) string {
	if len(value) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(value, &v); err != nil {
		return ""
	}
	out, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(out)
}

func summarizeObjectProperties(value json.RawMessage) string {
	rendered := compactJSON(value)
	if len(rendered) > 220 {
		return rendered[:220] + "..."
	}
	return rendered
}

func objectTitle(ot *models.ObjectType, object *ObjectInstance) string {
	if ot.PrimaryKeyProperty != nil {
		var props map[string]json.RawMessage
		if err := json.Unmarshal(object.Properties, &props); err == nil {
			if raw, ok := props[*ot.PrimaryKeyProperty]; ok {
				// Mirror Rust's two-arm match: a JSON string maps to its
				// inner value (whether empty or not). Anything else
				// renders via compact_json. Then a guard rejects an
				// empty result and falls through to the object id —
				// matching `Some(primary_key) if !primary_key.is_empty()`.
				var asString string
				if err := json.Unmarshal(raw, &asString); err == nil {
					if asString != "" {
						return ot.DisplayName + " · " + asString
					}
				} else {
					rendered := compactJSON(raw)
					if rendered != "" {
						return ot.DisplayName + " · " + rendered
					}
				}
			}
		}
	}
	return ot.DisplayName + " · " + object.ID.String()
}

// propertyTokensAndNames builds the body-string tokens for an object
// instance plus the set of property names. Iteration is alphabetical
// (Rust's serde_json::Map is BTreeMap-backed by default; Go map
// iteration is non-deterministic, so we sort keys before building the
// token string to match wire output across runs).
func propertyTokensAndNames(properties json.RawMessage) (string, []string) {
	if len(properties) == 0 {
		return "", nil
	}
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(properties, &asMap); err != nil {
		return "", nil
	}
	names := make([]string, 0, len(asMap))
	for name := range asMap {
		names = append(names, name)
	}
	sort.Strings(names)
	tokens := make([]string, 0, len(names))
	for _, name := range names {
		raw := asMap[name]
		var asString string
		if err := json.Unmarshal(raw, &asString); err == nil {
			tokens = append(tokens, name+": "+asString)
		} else {
			tokens = append(tokens, name+": "+compactJSON(raw))
		}
	}
	return joinSpace(tokens), names
}

func joinSpace(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += " " + p
	}
	return out
}

func uuidPtr(u uuid.UUID) *uuid.UUID { return &u }

func strDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}

// pgxRows is the minimal subset of pgx.Rows our scanners need. Local
// alias keeps the imports tight even when pgxpool changes its Query
// return type.
type pgxRows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
	Err() error
}
