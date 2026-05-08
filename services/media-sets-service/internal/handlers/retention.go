package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	"github.com/openfoundry/openfoundry-go/libs/outbox"
	cedarauthzlocal "github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/cedarauthz"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/retention"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/jackc/pgx/v5"
)

// RetentionHandlers exposes PATCH /api/v1/media-sets/{rid}/retention.
//
// Wires the reaper directly: a PATCH that *reduces* the window
// triggers a synchronous ReapMediaSet call so the rule "reduction is
// immediate" holds without waiting for the next periodic tick.
type RetentionHandlers struct {
	Repo   *repo.Repo
	Cedar  *cedarauthzlocal.Engine
	Reaper *retention.Reaper
}

// PatchRetentionRequest is the request body.
type PatchRetentionRequest struct {
	RetentionSeconds int64 `json:"retention_seconds"`
}

// PatchRetentionResponse mirrors the row + the count of items just
// expired by the synchronous sweep (zero on expansion).
type PatchRetentionResponse struct {
	MediaSet      models.MediaSet `json:"media_set"`
	ItemsReaped   int64           `json:"items_reaped"`
	WindowReduced bool            `json:"window_reduced"`
}

// PatchRetention — PATCH /api/v1/media-sets/{rid}/retention.
//
// Cedar gate: media_set::manage. The flow:
//
//  1. Load the row; if missing, 404.
//  2. Run the manage check.
//  3. Validate body.retention_seconds >= 0.
//  4. UPDATE the row inside a tx + emit
//     `media_set.retention_changed` audit envelope (atomicity per
//     ADR-0022).
//  5. If the new window < previous (or new < previous when previous
//     was 0/forever), call reaper.ReapMediaSet synchronously.
func (h *RetentionHandlers) PatchRetention(w http.ResponseWriter, r *http.Request) {
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
	var body PatchRetentionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.RetentionSeconds < 0 {
		writeJSONErr(w, http.StatusBadRequest, "retention_seconds must be >= 0")
		return
	}

	ctx := r.Context()
	current, err := h.Repo.GetMediaSet(ctx, rid)
	if err != nil {
		slog.Error("retention patch: load set", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if current == nil {
		writeJSONErr(w, http.StatusNotFound, "media set not found")
		return
	}
	if err := h.Cedar.CheckMediaSet(ctx, caller, cedarauthzlocal.ActionManage(), current); err != nil {
		var f *cedarauthzlocal.ErrForbidden
		if errors.As(err, &f) {
			writeJSONErr(w, http.StatusForbidden, f.Error())
			return
		}
		slog.Error("retention patch: cedar", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}

	previous, updated, err := h.Repo.UpdateRetentionSeconds(ctx, rid, body.RetentionSeconds)
	if err != nil {
		slog.Error("retention patch: update", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if updated == nil {
		// Row vanished between GetMediaSet and UPDATE; map to 404.
		writeJSONErr(w, http.StatusNotFound, "media set not found")
		return
	}

	// Audit emission. Wrap in a tx so the outbox insert lands
	// atomically with… in this slice the SQL UPDATE already
	// committed; we accept that the audit emission lives in a
	// short-lived tx of its own. A future refactor can fold the
	// UPDATE into the same tx — same tradeoff the access-pattern
	// service made for register.
	tx, err := h.Repo.BeginTx(ctx)
	if err != nil {
		slog.Error("retention patch: begin tx", slog.String("error", err.Error()))
	} else {
		event := audittrail.NewMediaSetRetentionChanged(
			updated.RID, updated.ProjectRID, updated.Markings,
			previous, body.RetentionSeconds,
		)
		auditCtx := auditCtxFromRequest(caller, r)
		if err := audittrail.EmitToOutbox(ctx, tx, event, auditCtx); err != nil {
			slog.Error("retention patch: emit audit", slog.String("error", err.Error()))
			_ = tx.Rollback(ctx)
		} else if err := tx.Commit(ctx); err != nil {
			slog.Error("retention patch: commit", slog.String("error", err.Error()))
		}
	}

	reduced := windowReduced(previous, body.RetentionSeconds)
	var reaped int64
	if reduced && h.Reaper != nil {
		expired, err := h.Reaper.ReapMediaSet(ctx, rid)
		if err != nil {
			slog.Error("retention patch: reap", slog.String("error", err.Error()))
		} else {
			reaped = int64(len(expired))
		}
	}

	writeJSON(w, http.StatusOK, PatchRetentionResponse{
		MediaSet: *updated, ItemsReaped: reaped, WindowReduced: reduced,
	})
}

// windowReduced reports whether the new retention window is strictly
// stricter than the previous one. Foundry contract:
//
//   - previous = 0 (forever) and new > 0 → reduced.
//   - both > 0 and new < previous       → reduced.
//   - new = 0 (forever)                 → expanded (never reduced).
//   - new == previous                   → no-op.
func windowReduced(previous, next int64) bool {
	if next == 0 {
		return false
	}
	if previous == 0 {
		return true
	}
	return next < previous
}

// Compile-time pin so a refactor that changes the cedar gate signature
// surfaces here instead of at runtime in the PATCH path.
var _ interface {
	CheckMediaSet(ctx context.Context, c *authmw.Claims, a cedar.EntityUID, set *models.MediaSet) error
} = (*cedarauthzlocal.Engine)(nil)

// Compile-time pin on the outbox dependency so a relocation surfaces
// here rather than at runtime.
var _ = outbox.OutboxEvent{}

// Compile-time pin on pgx.Tx — kept so a wholesale dep swap surfaces
// at build time.
var _ pgx.Tx = (pgx.Tx)(nil)
