package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	cedarauthzlocal "github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/cedarauthz"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/mediaitems"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/storage"
)

// MediaItemHandlers wires the media-item HTTP surface.
type MediaItemHandlers struct {
	Service *mediaitems.Service
}

// PresignUpload — POST /api/v1/media-sets/{rid}/items
func (h *MediaItemHandlers) PresignUpload(w http.ResponseWriter, r *http.Request) {
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
	var body models.PresignedUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	res, err := h.Service.PresignUpload(r.Context(), mediaitems.PresignUploadInput{
		MediaSetRID: rid,
		Body:        body,
		Claims:      caller,
		AuditCtx:    auditCtxFromRequest(caller, r),
	})
	if err != nil {
		writeMediaItemError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, models.PresignedURLBody{
		URL:       res.URL.URL,
		ExpiresAt: res.URL.ExpiresAt,
		Headers:   headerPairsToMap(res.URL.Headers),
		Item:      res.Item,
	})
}

// PresignDownload — GET /api/v1/items/{rid}/download
func (h *MediaItemHandlers) PresignDownload(w http.ResponseWriter, r *http.Request) {
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
	var ttl *uint64
	if raw := r.URL.Query().Get("expires_in_seconds"); raw != "" {
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, "expires_in_seconds must be a positive integer")
			return
		}
		ttl = &v
	}
	res, err := h.Service.PresignDownload(r.Context(), mediaitems.PresignDownloadInput{
		ItemRID:          rid,
		ExpiresInSeconds: ttl,
		Claims:           caller,
		AuditCtx:         auditCtxFromRequest(caller, r),
	})
	if err != nil {
		writeMediaItemError(w, err)
		return
	}
	headers := map[string]string{}
	for _, p := range res.URL.Headers {
		headers[p.Name] = p.Value
	}
	writeJSON(w, http.StatusOK, models.PresignedURLBody{
		URL:       res.URL.URL,
		ExpiresAt: res.URL.ExpiresAt,
		Headers:   headers,
		Item:      res.Item,
	})
}

// ListItems — GET /api/v1/media-sets/{rid}/items
func (h *MediaItemHandlers) ListItems(w http.ResponseWriter, r *http.Request) {
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
	q := r.URL.Query()
	branch := q.Get("branch")
	if branch == "" {
		branch = "main"
	}
	var prefix *string
	if p := q.Get("prefix"); p != "" {
		prefix = &p
	}
	var cursor *string
	if c := q.Get("cursor"); c != "" {
		cursor = &c
	}
	limit := 100
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			writeJSONErr(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = v
	}
	rows, err := h.Service.List(r.Context(), mediaitems.ListInput{
		MediaSetRID: rid, Branch: branch, Prefix: prefix, Cursor: cursor, Limit: limit, Claims: caller,
	})
	if err != nil {
		writeMediaItemError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.MediaItem]{Items: rows})
}

// GetItem — GET /api/v1/items/{rid}
func (h *MediaItemHandlers) GetItem(w http.ResponseWriter, r *http.Request) {
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
	item, err := h.Service.Get(r.Context(), caller, rid)
	if err != nil {
		writeMediaItemError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// DeleteItem — DELETE /api/v1/items/{rid}
func (h *MediaItemHandlers) DeleteItem(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Service.Delete(r.Context(), mediaitems.DeleteInput{
		ItemRID: rid, Claims: caller, AuditCtx: auditCtxFromRequest(caller, r),
	}); err != nil {
		writeMediaItemError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RegisterVirtualItem — POST /api/v1/media-sets/{rid}/virtual-items
func (h *MediaItemHandlers) RegisterVirtualItem(w http.ResponseWriter, r *http.Request) {
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
	var body models.RegisterVirtualItemRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	row, err := h.Service.RegisterVirtual(r.Context(), mediaitems.RegisterVirtualInput{
		MediaSetRID: rid, Body: body, Claims: caller, AuditCtx: auditCtxFromRequest(caller, r),
	})
	if err != nil {
		writeMediaItemError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, row)
}

// PatchMarkings — PATCH /api/v1/items/{rid}/markings
func (h *MediaItemHandlers) PatchMarkings(w http.ResponseWriter, r *http.Request) {
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
	var body models.PatchItemMarkingsRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	row, err := h.Service.PatchMarkings(r.Context(), mediaitems.PatchMarkingsInput{
		ItemRID: rid, Markings: body.Markings, Claims: caller, AuditCtx: auditCtxFromRequest(caller, r),
	})
	if err != nil {
		writeMediaItemError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

// writeMediaItemError maps the service-layer error categories to HTTP
// codes. Cedar denials surface as 403 with the precise missing
// markings; missing rows as 404; validation as 400; everything else
// as 500.
func writeMediaItemError(w http.ResponseWriter, err error) {
	var bad *mediaitems.ErrBadRequest
	var notFound *mediaitems.ErrNotFound
	var forbidden *cedarauthzlocal.ErrForbidden
	switch {
	case errors.As(err, &bad):
		writeJSONErr(w, http.StatusBadRequest, bad.Msg)
	case errors.As(err, &notFound):
		writeJSONErr(w, http.StatusNotFound, notFound.Error())
	case errors.As(err, &forbidden):
		writeJSONErr(w, http.StatusForbidden, forbidden.Error())
	default:
		slog.Error("media item handler", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
	}
}

func headerPairsToMap(in []storage.HeaderPair) map[string]string {
	out := map[string]string{}
	for _, p := range in {
		out[p.Name] = p.Value
	}
	return out
}
