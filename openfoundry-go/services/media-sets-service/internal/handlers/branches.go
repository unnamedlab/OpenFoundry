package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/branches"
	cedarauthzlocal "github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/cedarauthz"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
)

// BranchHandlers wires the branch HTTP surface.
type BranchHandlers struct {
	Service *branches.Service
}

// ListBranches — GET /api/v1/media-sets/{rid}/branches
func (h *BranchHandlers) ListBranches(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	rows, err := h.Service.List(r.Context(), branches.ListInput{MediaSetRID: rid, Claims: caller})
	if err != nil {
		writeBranchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.MediaSetBranch]{Items: rows})
}

// CreateBranch — POST /api/v1/media-sets/{rid}/branches
func (h *BranchHandlers) CreateBranch(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	if rid == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid required")
		return
	}
	var body models.CreateBranchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	row, err := h.Service.Create(r.Context(), branches.CreateInput{
		MediaSetRID: rid, Body: body, Claims: caller, AuditCtx: auditCtxFromRequest(caller, r),
	})
	if err != nil {
		writeBranchError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

// DeleteBranch — DELETE /api/v1/media-sets/{rid}/branches/{name}
func (h *BranchHandlers) DeleteBranch(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if rid == "" || name == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid and branch name required")
		return
	}
	if err := h.Service.Delete(r.Context(), branches.DeleteInput{
		MediaSetRID: rid, BranchName: name, Claims: caller, AuditCtx: auditCtxFromRequest(caller, r),
	}); err != nil {
		writeBranchError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ResetBranch — POST /api/v1/media-sets/{rid}/branches/{name}/reset
func (h *BranchHandlers) ResetBranch(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if rid == "" || name == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid and branch name required")
		return
	}
	resp, err := h.Service.Reset(r.Context(), branches.ResetInput{
		MediaSetRID: rid, BranchName: name, Claims: caller, AuditCtx: auditCtxFromRequest(caller, r),
	})
	if err != nil {
		writeBranchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// MergeBranch — POST /api/v1/media-sets/{rid}/branches/{name}/merge
func (h *BranchHandlers) MergeBranch(w http.ResponseWriter, r *http.Request) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rid := strings.TrimSpace(chi.URLParam(r, "rid"))
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if rid == "" || name == "" {
		writeJSONErr(w, http.StatusBadRequest, "rid and source branch name required")
		return
	}
	var body models.MergeBranchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	resp, err := h.Service.Merge(r.Context(), branches.MergeInput{
		MediaSetRID: rid, SourceBranch: name, Body: body, Claims: caller,
		AuditCtx: auditCtxFromRequest(caller, r),
	})
	if err != nil {
		writeBranchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// writeBranchError maps the service-layer error categories to HTTP
// codes. Cedar denials → 403; transactionless reset → 422; merge
// conflict → 409 with conflict_paths body; bad request → 400; not
// found → 404; everything else → 500.
func writeBranchError(w http.ResponseWriter, err error) {
	var bad *branches.ErrBadRequest
	var notFound *branches.ErrNotFound
	var transactionless *branches.ErrTransactionlessRejectsReset
	var conflict *branches.ErrMergeConflict
	var forbidden *cedarauthzlocal.ErrForbidden
	switch {
	case errors.As(err, &bad):
		writeJSONErr(w, http.StatusBadRequest, bad.Msg)
	case errors.As(err, &notFound):
		writeJSONErr(w, http.StatusNotFound, notFound.Error())
	case errors.As(err, &transactionless):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": transactionless.Error(),
			"code":  "MEDIA_SET_TRANSACTIONLESS_REJECTS_RESET",
		})
	case errors.As(err, &conflict):
		writeJSON(w, http.StatusConflict, models.MergeConflictBody{
			Error:         conflict.Error(),
			ConflictPaths: conflict.Paths,
		})
	case errors.As(err, &forbidden):
		writeJSONErr(w, http.StatusForbidden, forbidden.Error())
	default:
		slog.Error("branch handler", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
	}
}
