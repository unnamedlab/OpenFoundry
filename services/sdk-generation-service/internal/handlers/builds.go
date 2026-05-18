package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/repo"
)

// BuildHandlers wires the /api/v1/sdks/builds surface — the v0 OSDK
// endpoint that issues build IDs, exposes status, and streams the
// produced tarball.
type BuildHandlers struct {
	Repo      *repo.Repo
	Worker    *BuildWorker
	Artifacts BuildArtifactStore
	// SpawnAsync, when nil, dispatches the build with a fresh
	// background goroutine. Tests override it to run inline so they
	// don't have to poll for completion.
	SpawnAsync func(buildID uuid.UUID)
}

// CreateBuildResponse is what POST /api/v1/sdks/builds returns. We
// surface enough to compose the polling URL on the client.
type CreateBuildResponse struct {
	BuildID uuid.UUID     `json:"build_id"`
	Status  domain.Status `json:"status"`
}

// Create handles POST /api/v1/sdks/builds.
func (h *BuildHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var req domain.SDKRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeText(w, http.StatusBadRequest, "invalid body")
		return
	}
	target, err := domain.ParseTarget(string(req.Target))
	if err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Target = target
	// Default the tenant from claims when the caller omits it — we
	// still let an explicit tenant_id win for the admin-style "build
	// for another tenant" path. Tenancy enforcement on the read side
	// keeps that honest.
	claims, _ := authmw.FromContext(r.Context())
	if req.TenantID == uuid.Nil && claims != nil && claims.OrgID != nil {
		req.TenantID = *claims.OrgID
	}
	if err := req.Validate(); err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}

	build := &domain.SDKBuild{
		ID:              uuid.New(),
		TenantID:        req.TenantID,
		OntologyVersion: req.OntologyVersion,
		Target:          req.Target,
		Status:          domain.StatusQueued,
		CreatedAt:       time.Now().UTC(),
	}
	if claims != nil {
		build.RequestedBy = claims.Sub
	}
	if err := h.Repo.CreateBuild(r.Context(), build, req.IncludeObjectTypes, req.IncludeActionTypes); err != nil {
		slog.Error("create build failed", slog.String("error", err.Error()))
		writeText(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.dispatch(build.ID)
	writeJSON(w, http.StatusAccepted, CreateBuildResponse{BuildID: build.ID, Status: build.Status})
}

func (h *BuildHandlers) dispatch(id uuid.UUID) {
	if h.SpawnAsync != nil {
		h.SpawnAsync(id)
		return
	}
	if h.Worker == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := h.Worker.ProcessBuild(ctx, id); err != nil {
			slog.Error("process build failed",
				slog.String("build_id", id.String()),
				slog.String("error", err.Error()))
		}
	}()
}

// Get handles GET /api/v1/sdks/builds/{id}.
func (h *BuildHandlers) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeText(w, http.StatusBadRequest, "invalid id")
		return
	}
	build, err := h.Repo.GetBuild(r.Context(), id)
	if err != nil {
		writeText(w, http.StatusInternalServerError, err.Error())
		return
	}
	if build == nil {
		writeText(w, http.StatusNotFound, "not found")
		return
	}
	if !callerCanAccessTenant(r, build.TenantID) {
		writeText(w, http.StatusForbidden, "forbidden")
		return
	}
	writeJSON(w, http.StatusOK, build)
}

// Artifact handles GET /api/v1/sdks/builds/{id}/artifact. It streams
// the saved tar.gz back. Returns 409 when the build is not yet
// succeeded so callers don't accidentally cache an empty body.
func (h *BuildHandlers) Artifact(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeText(w, http.StatusBadRequest, "invalid id")
		return
	}
	build, err := h.Repo.GetBuild(r.Context(), id)
	if err != nil {
		writeText(w, http.StatusInternalServerError, err.Error())
		return
	}
	if build == nil {
		writeText(w, http.StatusNotFound, "not found")
		return
	}
	if !callerCanAccessTenant(r, build.TenantID) {
		writeText(w, http.StatusForbidden, "forbidden")
		return
	}
	if build.Status != domain.StatusSucceeded {
		writeText(w, http.StatusConflict, "build status: "+string(build.Status))
		return
	}
	body, err := h.Artifacts.Open(r.Context(), build.ArtifactURI)
	if err != nil {
		slog.Error("open artifact failed", slog.String("error", err.Error()))
		writeText(w, http.StatusInternalServerError, "artifact read failed")
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition",
		`attachment; filename="openfoundry-osdk-`+build.OntologyVersion+`.tgz"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// List handles GET /api/v1/sdks/builds. Query params: tenant_id,
// target, status, limit. Callers without admin role only see their
// own tenant.
func (h *BuildHandlers) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := repo.ListBuildsFilter{}
	if v := q.Get("tenant_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeText(w, http.StatusBadRequest, "invalid tenant_id")
			return
		}
		filter.TenantID = id
	}
	if v := q.Get("target"); v != "" {
		t, err := domain.ParseTarget(v)
		if err != nil {
			writeText(w, http.StatusBadRequest, err.Error())
			return
		}
		filter.Target = t
	}
	if v := q.Get("status"); v != "" {
		filter.Status = domain.Status(v)
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeText(w, http.StatusBadRequest, "invalid limit")
			return
		}
		filter.Limit = n
	}

	claims, _ := authmw.FromContext(r.Context())
	if claims != nil && !claims.HasRole("admin") {
		if claims.OrgID == nil {
			writeText(w, http.StatusForbidden, "no tenant")
			return
		}
		if filter.TenantID != uuid.Nil && filter.TenantID != *claims.OrgID {
			writeText(w, http.StatusForbidden, "cross-tenant list forbidden")
			return
		}
		filter.TenantID = *claims.OrgID
	}

	builds, err := h.Repo.ListBuilds(r.Context(), filter)
	if err != nil {
		writeText(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, builds)
}

func callerCanAccessTenant(r *http.Request, buildTenant uuid.UUID) bool {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		return false
	}
	if claims.HasRole("admin") {
		return true
	}
	if claims.OrgID == nil {
		return false
	}
	return *claims.OrgID == buildTenant
}
