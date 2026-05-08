// Markings CRUD endpoints. Wraps `internal/repo` markings projections
// with HTTP semantics + the authz engine.
//
//   - GET .../namespaces/{ns}/markings
//   - POST .../namespaces/{ns}/markings — replace explicit markings.
//     Requires `iceberg::namespace::manage_markings`.
//   - GET .../namespaces/{ns}/tables/{tbl}/markings
//   - PATCH .../namespaces/{ns}/tables/{tbl}/markings — replace
//     explicit markings on a table. Requires
//     `iceberg::table::manage_markings`.
//
// The handlers depend on the bearer extractor having injected an
// `auth.AuthenticatedPrincipal` into context — the routes in
// `internal/server` are wrapped accordingly.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/audit"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/authz"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/domain/markings"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/handlers/auth"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/models"
)

// MarkingsStore is the contract MarkingsHandlers needs from the data
// layer. It bundles namespace + table lookup with the markings-specific
// projections + mutators.
type MarkingsStore interface {
	GetNamespaceByProjectName(ctx context.Context, projectRID, name string) (*models.IcebergNamespace, error)
	GetTable(ctx context.Context, projectRID string, namespace []string, tableName string) (*models.IcebergTable, error)
	LoadNamespaceMarkings(ctx context.Context, namespaceID uuid.UUID) (*markings.NamespaceMarkings, error)
	LoadTableMarkings(ctx context.Context, tableID uuid.UUID) (*markings.TableMarkings, error)
	SetNamespaceMarkings(ctx context.Context, namespaceID uuid.UUID, ids []uuid.UUID, actor uuid.UUID) (*markings.NamespaceMarkings, error)
	SetTableExplicitMarkings(ctx context.Context, tableID uuid.UUID, ids []uuid.UUID, actor uuid.UUID) (*markings.TableMarkings, error)
	ResolveMarkingName(ctx context.Context, name string) (uuid.UUID, error)
}

// MarkingsHandlers wires the markings endpoints. Held by the server +
// constructed in `cmd/iceberg-catalog-service/main.go`.
type MarkingsHandlers struct {
	Store         MarkingsStore
	Authz         authz.Engine
	DefaultTenant string
}

// UpdateMarkingsRequest is the body of POST/PATCH markings endpoints.
// `markings` is the replacement set; unknown names yield 400.
type UpdateMarkingsRequest struct {
	Markings []string `json:"markings"`
}

// GetNamespaceMarkings serves GET .../namespaces/{ns}/markings.
func (m *MarkingsHandlers) GetNamespaceMarkings(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	ns, ok := m.lookupNamespace(w, r)
	if !ok {
		return
	}
	projection, err := m.Store.LoadNamespaceMarkings(r.Context(), ns.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	resource := authz.NamespaceResource(authz.NamespaceAttrs{
		RID:        fmt.Sprintf("ri.foundry.main.iceberg-namespace.%s", ns.ID),
		ProjectRID: ns.ProjectRID,
		Tenant:     m.DefaultTenant,
		Name:       ns.Name,
		Markings:   markings.Names(projection.Effective),
	})
	if err := m.Authz.Enforce(r.Context(), principal.AsAuthzPrincipal(), "iceberg::namespace::view", resource); err != nil {
		writeAuthzError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projection)
}

// UpdateNamespaceMarkings serves POST .../namespaces/{ns}/markings.
func (m *MarkingsHandlers) UpdateNamespaceMarkings(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	ns, ok := m.lookupNamespace(w, r)
	if !ok {
		return
	}
	before, err := m.Store.LoadNamespaceMarkings(r.Context(), ns.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	resource := authz.NamespaceResource(authz.NamespaceAttrs{
		RID:        fmt.Sprintf("ri.foundry.main.iceberg-namespace.%s", ns.ID),
		ProjectRID: ns.ProjectRID,
		Tenant:     m.DefaultTenant,
		Name:       ns.Name,
		Markings:   markings.Names(before.Effective),
	})
	if err := m.Authz.Enforce(r.Context(), principal.AsAuthzPrincipal(), "iceberg::namespace::manage_markings", resource); err != nil {
		writeAuthzError(w, err)
		return
	}
	var body UpdateMarkingsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	ids, err := m.resolveMarkingIDs(r.Context(), body.Markings)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	actor := parseActor(principal)
	after, err := m.Store.SetNamespaceMarkings(r.Context(), ns.ID, ids, actor)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	audit.MarkingsUpdated(
		actor,
		fmt.Sprintf("ri.foundry.main.iceberg-namespace.%s", ns.ID),
		"namespace",
		markings.Names(before.Effective),
		markings.Names(after.Effective),
	)
	writeJSON(w, http.StatusOK, after)
}

// GetTableMarkings serves GET .../namespaces/{ns}/tables/{tbl}/markings.
func (m *MarkingsHandlers) GetTableMarkings(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	tab, ok := m.lookupTable(w, r)
	if !ok {
		return
	}
	projection, err := m.Store.LoadTableMarkings(r.Context(), tab.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	resource := authz.TableResource(authz.TableAttrs{
		RID:              tab.RID,
		NamespaceRID:     fmt.Sprintf("ri.foundry.main.iceberg-namespace.%s", tab.NamespaceID),
		Tenant:           m.DefaultTenant,
		FormatVersion:    tab.FormatVersion,
		Markings:         markings.Names(projection.Effective),
		ExplicitMarkings: markings.Names(projection.Explicit),
	})
	if err := m.Authz.Enforce(r.Context(), principal.AsAuthzPrincipal(), "iceberg::table::view", resource); err != nil {
		writeAuthzError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projection)
}

// UpdateTableMarkings serves PATCH .../tables/{tbl}/markings.
func (m *MarkingsHandlers) UpdateTableMarkings(w http.ResponseWriter, r *http.Request) {
	principal, ok := requirePrincipal(w, r)
	if !ok {
		return
	}
	tab, ok := m.lookupTable(w, r)
	if !ok {
		return
	}
	before, err := m.Store.LoadTableMarkings(r.Context(), tab.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	resource := authz.TableResource(authz.TableAttrs{
		RID:              tab.RID,
		NamespaceRID:     fmt.Sprintf("ri.foundry.main.iceberg-namespace.%s", tab.NamespaceID),
		Tenant:           m.DefaultTenant,
		FormatVersion:    tab.FormatVersion,
		Markings:         markings.Names(before.Effective),
		ExplicitMarkings: markings.Names(before.Explicit),
	})
	if err := m.Authz.Enforce(r.Context(), principal.AsAuthzPrincipal(), "iceberg::table::manage_markings", resource); err != nil {
		writeAuthzError(w, err)
		return
	}
	var body UpdateMarkingsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	ids, err := m.resolveMarkingIDs(r.Context(), body.Markings)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	actor := parseActor(principal)
	after, err := m.Store.SetTableExplicitMarkings(r.Context(), tab.ID, ids, actor)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	beforeExplicit := markings.Names(before.Explicit)
	for _, name := range markings.Names(after.Explicit) {
		if !stringSliceContains(beforeExplicit, name) {
			audit.MarkingsOverrideCreated(actor, tab.RID, name)
		}
	}
	audit.MarkingsUpdated(
		actor,
		tab.RID,
		"table",
		markings.Names(before.Effective),
		markings.Names(after.Effective),
	)
	writeJSON(w, http.StatusOK, after)
}

func (m *MarkingsHandlers) lookupNamespace(w http.ResponseWriter, r *http.Request) (*models.IcebergNamespace, bool) {
	path := joinNamespacePath(namespacePath(chi.URLParam(r, "namespace")))
	ns, err := m.Store.GetNamespaceByProjectName(r.Context(), projectRID(r), path)
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

func (m *MarkingsHandlers) lookupTable(w http.ResponseWriter, r *http.Request) (*models.IcebergTable, bool) {
	tab, err := m.Store.GetTable(r.Context(), projectRID(r), namespacePath(chi.URLParam(r, "namespace")), chi.URLParam(r, "table"))
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if tab == nil {
		writeJSONErr(w, http.StatusNotFound, "table not found")
		return nil, false
	}
	return tab, true
}

func (m *MarkingsHandlers) resolveMarkingIDs(ctx context.Context, names []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(names))
	for _, name := range names {
		id, err := m.Store.ResolveMarkingName(ctx, name)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func requirePrincipal(w http.ResponseWriter, r *http.Request) (*auth.AuthenticatedPrincipal, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return nil, false
	}
	return p, true
}

func writeAuthzError(w http.ResponseWriter, err error) {
	var deny *authz.DenyError
	if errors.As(err, &deny) {
		writeJSONErr(w, http.StatusForbidden, deny.Error())
		return
	}
	writeJSONErr(w, http.StatusInternalServerError, err.Error())
}

func parseActor(principal *auth.AuthenticatedPrincipal) uuid.UUID {
	if principal == nil {
		return uuid.Nil
	}
	if id, err := uuid.Parse(principal.Subject); err == nil {
		return id
	}
	return uuid.Nil
}

func stringSliceContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func joinNamespacePath(parts []string) string {
	return strings.Join(parts, ".")
}
