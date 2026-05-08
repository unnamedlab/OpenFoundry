// `POST /api/v1/workflows/approvals/{id}/continue` — the legacy
// "continue after approval" route. Pre-S8 it proxied to a separate
// approvals-service; post-S8 the state machine lives in this crate
// and the route applies the decision in-process.

package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/approvals"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/models"
)

// ContinueAfterApproval ports `handlers::approvals::continue_after_approval`.
func (h *CrudHandlers) ContinueAfterApproval(w http.ResponseWriter, r *http.Request) {
	approvalID, err := uuid.Parse(chi.URLParam(r, "approval_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "approval_id must be a uuid")
		return
	}
	var body models.InternalApprovalContinuationRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	event, err := decisionEventFromContinuation(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := approvals.ApplyDecisionAndPublish(r.Context(), h.State, approvalID, event); err != nil {
		slog.Error("approval continuation failed",
			slog.String("approval_id", approvalID.String()),
			slog.String("error", err.Error()))
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "transition") || strings.Contains(err.Error(), "not found") {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func decisionEventFromContinuation(body models.InternalApprovalContinuationRequest) (approvals.Event, error) {
	approver := "system"
	if len(body.Context) > 0 {
		var holder map[string]any
		if err := json.Unmarshal(body.Context, &holder); err == nil {
			if v, ok := holder["approver"].(string); ok && v != "" {
				approver = v
			}
		}
	}
	var comment *string
	if len(body.Context) > 0 {
		var holder map[string]any
		if err := json.Unmarshal(body.Context, &holder); err == nil {
			if v, ok := holder["comment"].(string); ok && v != "" {
				out := v
				comment = &out
			}
		}
	}
	switch strings.ToLower(body.Decision) {
	case "approve", "approved":
		return approvals.Event{Kind: approvals.EventApprove, DecidedBy: approver, Comment: comment}, nil
	case "reject", "rejected":
		return approvals.Event{Kind: approvals.EventReject, DecidedBy: approver, Comment: comment}, nil
	default:
		return approvals.Event{}, fmt.Errorf("unsupported approval decision '%s'", body.Decision)
	}
}
