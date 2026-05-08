package handlers

// IRF-8 — Stream branches REST surface (Bloque E1). Mirrors
// services/ingestion-replication-service/src/event_streaming/handlers/branches.rs.
//
//   GET    /api/v1/streaming/streams/{id}/branches
//   POST   /api/v1/streaming/streams/{id}/branches
//   GET    /api/v1/streaming/streams/{id}/branches/{name}
//   DELETE /api/v1/streaming/streams/{id}/branches/{name}
//   POST   /api/v1/streaming/streams/{id}/branches/{name}:merge
//   POST   /api/v1/streaming/streams/{id}/branches/{name}:archive
//
// Cold-branch materialisation delegates to dataset-versioning-service
// over HTTP using the injected ColdTierBridge — failures only surface
// in the response message (the local row is always written first).

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/domain/streambranch"
)

// BranchesHandler bundles the six stream-branch endpoints.
type BranchesHandler struct {
	Store      streambranch.Store
	Cold       streambranch.ColdTierBridge
	MetricSink BranchMetricSink
}

// BranchMetricSink mirrors the Rust `metrics.record_stream_rows_archived`
// hook so the cold archive counter stays connected when the operator
// wires the production observability stack. nil disables emission.
type BranchMetricSink interface {
	RecordStreamRowsArchived(branch string, rows uint64)
}

// MainBranchName is the protected branch name. Mirrors Rust's literal
// "main" check inside delete_branch.
const MainBranchName = "main"

// ListBranches is GET /streams/{id}/branches.
func (h *BranchesHandler) ListBranches(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireClaims(w, r); !ok {
		return
	}
	streamID, ok := parseStreamID(w, r)
	if !ok {
		return
	}
	exists, err := h.Store.StreamExists(r.Context(), streamID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if !exists {
		writeJSONErr(w, http.StatusNotFound, "stream not found")
		return
	}
	items, err := h.Store.ListBranches(r.Context(), streamID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if items == nil {
		items = []streambranch.StreamBranch{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

// CreateBranch is POST /streams/{id}/branches.
func (h *BranchesHandler) CreateBranch(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	if !canWriteStreams(claims) {
		writeJSONErr(w, http.StatusForbidden, "caller lacks 'streaming:write' permission")
		return
	}
	streamID, ok := parseStreamID(w, r)
	if !ok {
		return
	}
	var body streambranch.CreateBranchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeJSONErr(w, http.StatusBadRequest, "branch name is required")
		return
	}
	if !validBranchName(name) {
		writeJSONErr(w, http.StatusBadRequest,
			"branch name must contain only alphanumerics, '-', '_' or '/'")
		return
	}
	exists, err := h.Store.StreamExists(r.Context(), streamID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if !exists {
		writeJSONErr(w, http.StatusNotFound, "stream not found")
		return
	}
	if body.ParentBranchID != nil {
		ok, err := h.Store.ParentBranchBelongsTo(r.Context(), *body.ParentBranchID, streamID)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
			return
		}
		if !ok {
			writeJSONErr(w, http.StatusBadRequest, "parent_branch_id is not part of this stream")
			return
		}
	}
	description := ""
	if body.Description != nil {
		description = *body.Description
	}
	branch, err := h.Store.CreateBranch(
		r.Context(), streamID, name, branchActor(claims),
		body.ParentBranchID, body.DatasetBranchID, description,
	)
	if err != nil {
		slog.Error("create branch", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	emitBranchAudit(claims, "streaming.branch.created", branch.ID,
		slog.String("stream_id", streamID.String()),
		slog.String("name", branch.Name))
	writeJSON(w, http.StatusOK, branch)
}

// GetBranch is GET /streams/{id}/branches/{name}.
func (h *BranchesHandler) GetBranch(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireClaims(w, r); !ok {
		return
	}
	streamID, name, ok := parseStreamAndName(w, r)
	if !ok {
		return
	}
	branch, err := h.Store.GetBranchByName(r.Context(), streamID, name)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if branch == nil {
		writeJSONErr(w, http.StatusNotFound, "branch not found")
		return
	}
	writeJSON(w, http.StatusOK, branch)
}

// DeleteBranch is DELETE /streams/{id}/branches/{name}. Hard-deletes
// the row when (a) the name is not "main", and (b) the branch is empty
// (head_sequence_no == 0) or already terminated (status in
// {merged, archived}).
func (h *BranchesHandler) DeleteBranch(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	if !canWriteStreams(claims) {
		writeJSONErr(w, http.StatusForbidden, "caller lacks 'streaming:write' permission")
		return
	}
	streamID, name, ok := parseStreamAndName(w, r)
	if !ok {
		return
	}
	if name == MainBranchName {
		writeJSONErr(w, http.StatusBadRequest, "cannot delete the 'main' branch")
		return
	}
	branch, err := h.Store.GetBranchByName(r.Context(), streamID, name)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if branch == nil {
		writeJSONErr(w, http.StatusNotFound, "branch not found")
		return
	}
	if branch.HeadSequenceNo > 0 && branch.Status != "merged" && branch.Status != "archived" {
		writeJSONErr(w, http.StatusBadRequest,
			"branch has uncommitted history; archive or merge it first")
		return
	}
	if err := h.Store.DeleteBranch(r.Context(), branch.ID); err != nil {
		slog.Error("delete branch", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	emitBranchAudit(claims, "streaming.branch.deleted", branch.ID,
		slog.String("stream_id", streamID.String()),
		slog.String("name", name))
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": true,
		"id":      branch.ID,
	})
}

// MergeBranch is POST /streams/{id}/branches/{name}:merge.
func (h *BranchesHandler) MergeBranch(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	if !canWriteStreams(claims) {
		writeJSONErr(w, http.StatusForbidden, "caller lacks 'streaming:write' permission")
		return
	}
	streamID, name, ok := parseStreamAndName(w, r)
	if !ok {
		return
	}
	var body streambranch.MergeBranchRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONErr(w, http.StatusBadRequest, "invalid body")
			return
		}
	}
	target := MainBranchName
	if body.TargetBranch != nil && *body.TargetBranch != "" {
		target = *body.TargetBranch
	}
	if target == name {
		writeJSONErr(w, http.StatusBadRequest, "target branch must differ from source")
		return
	}
	source, err := h.Store.GetBranchByName(r.Context(), streamID, name)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if source == nil {
		writeJSONErr(w, http.StatusNotFound, "source branch not found")
		return
	}
	targetBranch, err := h.Store.GetBranchByName(r.Context(), streamID, target)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if targetBranch == nil {
		writeJSONErr(w, http.StatusNotFound, "target branch not found")
		return
	}
	merged := source.HeadSequenceNo
	if targetBranch.HeadSequenceNo > merged {
		merged = targetBranch.HeadSequenceNo
	}
	if err := h.Store.MergeBranches(r.Context(), source.ID, targetBranch.ID, merged); err != nil {
		slog.Error("merge branches", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	emitBranchAudit(claims, "streaming.branch.merged", source.ID,
		slog.String("stream_id", streamID.String()),
		slog.String("source", name),
		slog.String("target", target),
		slog.Int64("merged_sequence_no", merged))
	writeJSON(w, http.StatusOK, streambranch.MergeBranchResponse{
		SourceBranchID:   source.ID,
		TargetBranchID:   targetBranch.ID,
		MergedSequenceNo: merged,
		Message:          "merged '" + name + "' into '" + target + "'",
	})
}

// ArchiveBranch is POST /streams/{id}/branches/{name}:archive.
func (h *BranchesHandler) ArchiveBranch(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireClaims(w, r)
	if !ok {
		return
	}
	if !canWriteStreams(claims) {
		writeJSONErr(w, http.StatusForbidden, "caller lacks 'streaming:write' permission")
		return
	}
	streamID, name, ok := parseStreamAndName(w, r)
	if !ok {
		return
	}
	var body streambranch.ArchiveBranchRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONErr(w, http.StatusBadRequest, "invalid body")
			return
		}
	}
	current, err := h.Store.GetBranchByName(r.Context(), streamID, name)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if current == nil {
		writeJSONErr(w, http.StatusNotFound, "branch not found")
		return
	}
	updated, err := h.Store.ArchiveBranch(r.Context(), streamID, current.ID, name)
	if err != nil {
		slog.Error("archive branch", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "database operation failed")
		return
	}
	if updated == nil {
		writeJSONErr(w, http.StatusNotFound, "branch not found")
		return
	}

	// Best-effort cold-tier commit. Failures never surface — the local
	// row is already archived; operators can retry via the dataset-
	// versioning UI. Mirrors the Rust handler's warn-and-continue path.
	if body.CommitCold {
		if updated.DatasetBranchID != nil && *updated.DatasetBranchID != "" {
			h.commitColdBestEffort(r.Context(), updated)
		} else {
			slog.Info("cold tier commit skipped: no dataset_branch_id",
				slog.String("branch_id", updated.ID.String()))
		}
	}

	emitBranchAudit(claims, "streaming.branch.archived", current.ID,
		slog.String("stream_id", streamID.String()),
		slog.String("name", name),
		slog.Bool("commit_cold", body.CommitCold))
	writeJSON(w, http.StatusOK, updated)
}

func (h *BranchesHandler) commitColdBestEffort(ctx context.Context, branch *streambranch.StreamBranch) {
	if h.Cold == nil {
		slog.Info("cold tier bridge not configured; skipping cold commit",
			slog.String("branch_id", branch.ID.String()))
		return
	}
	accepted, err := h.Cold.CommitCold(ctx, branch, time.Now().UTC())
	if err != nil {
		slog.Warn("cold tier commit failed",
			slog.String("branch_id", branch.ID.String()),
			slog.String("error", err.Error()))
		return
	}
	if !accepted {
		slog.Warn("cold tier commit rejected",
			slog.String("branch_id", branch.ID.String()))
		return
	}
	slog.Info("cold tier commit accepted",
		slog.String("branch_id", branch.ID.String()))
	if h.MetricSink != nil && branch.HeadSequenceNo > 0 {
		h.MetricSink.RecordStreamRowsArchived(branch.Name, uint64(branch.HeadSequenceNo))
	}
}

func parseStreamAndName(w http.ResponseWriter, r *http.Request) (uuid.UUID, string, bool) {
	streamID, ok := parseStreamID(w, r)
	if !ok {
		return uuid.Nil, "", false
	}
	name := chi.URLParam(r, "name")
	if name == "" {
		writeJSONErr(w, http.StatusBadRequest, "branch name is required")
		return uuid.Nil, "", false
	}
	return streamID, name, true
}

// validBranchName allows ASCII alphanumerics plus '-', '_', '/' —
// matches the Rust `c.is_ascii_alphanumeric() || c == '-' || c == '_' || c == '/'`.
func validBranchName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c == '-' || c == '_' || c == '/':
		default:
			return false
		}
	}
	return true
}

func branchActor(c *authmw.Claims) string {
	if c.Email != "" {
		return c.Email
	}
	return c.Sub.String()
}

func emitBranchAudit(c *authmw.Claims, action string, resourceID uuid.UUID, extra ...slog.Attr) {
	attrs := []any{
		slog.String("action", action),
		slog.String("actor_sub", c.Sub.String()),
		slog.String("actor_email", c.Email),
		slog.String("resource", "streaming_stream_branch"),
		slog.String("resource_id", resourceID.String()),
	}
	for _, a := range extra {
		attrs = append(attrs, a)
	}
	slog.Info("audit "+action, attrs...)
}

// Sentinel kept here so cold bridges that need to flag a "branch was
// already archived" race can reach a typed error if needed in future.
var ErrBranchAlreadyArchived = errors.New("stream branch already archived")
