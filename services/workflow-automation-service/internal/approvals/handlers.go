// HTTP handlers for the approvals subsystem.
//
// `POST /api/v1/approvals`           — create_approval
// `GET  /api/v1/approvals`           — list_approvals
// `POST /api/v1/approvals/{id}/decide` — decide_approval
//
// Plus `apply_decision_and_publish` reused by the workflows
// continue-after-approval handler.

package approvals

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/outbox"
	statemachine "github.com/openfoundry/openfoundry-go/libs/state-machine"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/state"
)

// Handlers wires AppState into the approvals HTTP endpoints.
type Handlers struct {
	State *state.AppState
}

// NewHandlers wires the approvals handlers.
func NewHandlers(s *state.AppState) *Handlers { return &Handlers{State: s} }

// CreateApproval ports `handlers::approvals::create_approval`.
func (h *Handlers) CreateApproval(w http.ResponseWriter, r *http.Request) {
	var body CreateApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	approval := approvalProjectionFromRequest(&body)
	aggregate := aggregateFromProjection(&approval, &body, h.State.ApprovalTTLHours)

	if err := insertStateMachineRowAndOutbox(r.Context(), h.State, aggregate); err != nil {
		slog.Error("approval state-machine insert failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, CreateApprovalResponse{
		Approval: withStateMetadata(approval, aggregate),
		Created:  true,
	})
}

// ListApprovals ports `handlers::approvals::list_approvals`.
func (h *Handlers) ListApprovals(w http.ResponseWriter, r *http.Request) {
	q := parseListQuery(r)
	page := derefInt64(q.Page, 1)
	if page < 1 {
		page = 1
	}
	perPage := clampInt64(derefInt64(q.PerPage, 20), 1, 100)
	offset := (page - 1) * perPage

	var rows pgx.Rows
	var err error
	if q.Status != nil {
		rows, err = h.State.DB.Query(r.Context(),
			`SELECT id, tenant_id, subject, assigned_to, decided_by, state, expires_at,
			        correlation_id, created_at, updated_at
			   FROM audit_compliance.approval_requests
			  WHERE state = $1
			  ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
			*q.Status, perPage, offset,
		)
	} else {
		rows, err = h.State.DB.Query(r.Context(),
			`SELECT id, tenant_id, subject, assigned_to, decided_by, state, expires_at,
			        correlation_id, created_at, updated_at
			   FROM audit_compliance.approval_requests
			  ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
			perPage, offset,
		)
	}
	if err != nil {
		slog.Error("list_approvals query failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	type row struct {
		ID            uuid.UUID  `json:"id"`
		TenantID      string     `json:"tenant_id"`
		Subject       string     `json:"subject"`
		AssignedTo    *uuid.UUID `json:"assigned_to"`
		DecidedBy     *uuid.UUID `json:"decided_by"`
		State         string     `json:"state"`
		ExpiresAt     *time.Time `json:"expires_at"`
		CorrelationID uuid.UUID  `json:"correlation_id"`
		CreatedAt     time.Time  `json:"created_at"`
		UpdatedAt     time.Time  `json:"updated_at"`
	}
	out := make([]row, 0)
	for rows.Next() {
		var rr row
		if err := rows.Scan(
			&rr.ID, &rr.TenantID, &rr.Subject, &rr.AssignedTo, &rr.DecidedBy,
			&rr.State, &rr.ExpiresAt, &rr.CorrelationID, &rr.CreatedAt, &rr.UpdatedAt,
		); err != nil {
			slog.Error("list_approvals scan failed", slog.String("error", err.Error()))
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, rr)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":     out,
		"page":     page,
		"per_page": perPage,
		"filters": map[string]any{
			"status":      q.Status,
			"assigned_to": q.AssignedTo,
			"workflow_id": q.WorkflowID,
		},
	})
}

// DecideApproval ports `handlers::approvals::decide_approval`.
func (h *Handlers) DecideApproval(w http.ResponseWriter, r *http.Request) {
	approvalID, err := uuid.Parse(chi.URLParam(r, "approval_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "approval_id must be a uuid")
		return
	}
	claims, _ := authmw.FromContext(r.Context())
	if claims == nil {
		writeError(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var body ApprovalDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	event, err := DecisionEventFromBody(body, claims.Sub)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := ApplyDecisionAndPublish(r.Context(), h.State, approvalID, event); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "transition") || strings.Contains(err.Error(), "not found") {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// ApplyDecisionAndPublish ports `apply_decision_and_publish`. Exported
// because the workflows continue-after-approval handler reuses it.
func ApplyDecisionAndPublish(ctx context.Context, s *state.AppState, approvalID uuid.UUID, event Event) error {
	store := statemachine.NewPgStore[*ApprovalRequest, Event](s.DB, TableName,
		func() *ApprovalRequest { return &ApprovalRequest{} })

	loaded, err := store.Load(ctx, approvalID)
	if err != nil {
		if statemachine.IsNotFound(err) {
			return fmt.Errorf("approval %s not found", approvalID)
		}
		return err
	}
	nextLoaded, err := store.Apply(ctx, loaded, event)
	if err != nil {
		return err
	}
	aggregate := nextLoaded.Machine

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var decision string
	switch aggregate.StateField {
	case StateApproved:
		decision = "approved"
	case StateRejected:
		decision = "rejected"
	default:
		return fmt.Errorf("approval landed in unexpected state %s after decide", aggregate.StateField)
	}
	decidedBy := "system"
	if aggregate.DecidedBy != nil {
		decidedBy = *aggregate.DecidedBy
	}
	decidedAt := time.Now().UTC()
	if aggregate.DecidedAt != nil {
		decidedAt = *aggregate.DecidedAt
	}
	completed := ApprovalCompletedV1Payload{
		ApprovalID:    aggregate.ID,
		TenantID:      aggregate.TenantID,
		CorrelationID: aggregate.CorrelationID,
		Decision:      decision,
		DecidedBy:     decidedBy,
		Comment:       aggregate.Comment,
		DecidedAt:     decidedAt,
	}
	if err := enqueueOutbox(ctx, tx, aggregate.ID, "completed", ApprovalCompletedV1, completed); err != nil {
		return err
	}

	// Mirror the decided_by column projection so list/get see the
	// decider without round-tripping through state_data.
	if aggregate.DecidedBy != nil {
		if decidedByUUID, err := uuid.Parse(*aggregate.DecidedBy); err == nil {
			if _, err := tx.Exec(ctx,
				`UPDATE audit_compliance.approval_requests SET decided_by = $1 WHERE id = $2`,
				decidedByUUID, aggregate.ID,
			); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if err := postAuditEvent(ctx, s, aggregate); err != nil {
		slog.Warn("audit event POST failed; outbox event will eventually replay it",
			slog.String("approval_id", aggregate.ID.String()), slog.String("error", err.Error()))
	}
	return nil
}

// DecisionEventFromBody mirrors `decision_event_from_body`. Exported
// because the workflows continue-after-approval handler reuses it.
func DecisionEventFromBody(body ApprovalDecisionRequest, actor uuid.UUID) (Event, error) {
	comment := body.Comment
	switch strings.ToLower(body.Decision) {
	case "approve", "approved":
		return Event{Kind: EventApprove, DecidedBy: actor.String(), Comment: comment}, nil
	case "reject", "rejected":
		return Event{Kind: EventReject, DecidedBy: actor.String(), Comment: comment}, nil
	default:
		return Event{}, fmt.Errorf("unsupported approval decision '%s'", body.Decision)
	}
}

func insertStateMachineRowAndOutbox(ctx context.Context, s *state.AppState, aggregate *ApprovalRequest) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	payload, err := json.Marshal(aggregate)
	if err != nil {
		return err
	}
	assignedTo := firstApproverUUID(aggregate.ApproverSet)

	if _, err := tx.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s
		      (id, tenant_id, subject, assigned_to, decided_by, state, state_data, version,
		       expires_at, correlation_id, created_at, updated_at)
		    VALUES ($1, $2, $3, $4, NULL, $5, $6, 1, $7, $8, now(), now())
		    ON CONFLICT (id) DO NOTHING`,
		TableName,
	),
		aggregate.ID, aggregate.TenantID, aggregate.Subject, assignedTo,
		aggregate.CurrentState(), payload, aggregate.ExpiresAt(), aggregate.CorrelationID,
	); err != nil {
		return fmt.Errorf("approval_requests insert failed: %w", err)
	}

	requested := ApprovalRequestedV1Payload{
		ApprovalID:    aggregate.ID,
		TenantID:      aggregate.TenantID,
		Subject:       aggregate.Subject,
		ApproverSet:   aggregate.ApproverSet,
		ActionPayload: aggregate.ActionPayload,
		CorrelationID: aggregate.CorrelationID,
		TriggeredBy:   "api",
		ExpiresAt:     aggregate.ExpiresAtField,
	}
	if err := enqueueOutbox(ctx, tx, aggregate.ID, "requested", ApprovalRequestedV1, requested); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func enqueueOutbox(ctx context.Context, tx pgx.Tx, approvalID uuid.UUID, kind, topic string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	eventID := DeriveOutboxEventID(approvalID, kind)
	out := outbox.New(eventID, "approval_request", approvalID.String(), topic, body).
		WithHeader("x-audit-correlation-id", approvalID.String()).
		WithHeader("ol-job", "approvals/decide").
		WithHeader("ol-run-id", approvalID.String()).
		WithHeader("ol-producer", "approvals-service")
	return outbox.Enqueue(ctx, tx, out)
}

func postAuditEvent(ctx context.Context, s *state.AppState, aggregate *ApprovalRequest) error {
	var action string
	switch aggregate.StateField {
	case StateApproved:
		action = "approval.approved"
	case StateRejected:
		action = "approval.rejected"
	case StateExpired:
		action = "approval.expired"
	default:
		return fmt.Errorf("unsupported terminal state %s", aggregate.StateField)
	}
	actor := "system"
	if aggregate.DecidedBy != nil {
		actor = *aggregate.DecidedBy
	}
	occurredAt := time.Now().UTC()
	if aggregate.DecidedAt != nil {
		occurredAt = *aggregate.DecidedAt
	}
	payload := map[string]any{
		"occurred_at":           occurredAt,
		"tenant_id":             aggregate.TenantID,
		"actor":                 actor,
		"action":                action,
		"resource_type":         "approval_request",
		"resource_id":           aggregate.ID,
		"audit_correlation_id":  aggregate.CorrelationID,
		"attributes": map[string]any{
			"subject":       aggregate.Subject,
			"approver_set":  aggregate.ApproverSet,
			"comment":       aggregate.Comment,
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := strings.TrimRight(s.AuditComplianceServiceURL, "/") + "/api/v1/audit/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-audit-correlation-id", aggregate.CorrelationID.String())
	if s.AuditComplianceBearerToken != "" {
		token := s.AuditComplianceBearerToken
		if !strings.HasPrefix(strings.ToLower(token), "bearer ") {
			token = "Bearer " + token
		}
		req.Header.Set("authorization", token)
	}
	client := s.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("audit POST failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("audit POST returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func approvalProjectionFromRequest(body *CreateApprovalRequest) WorkflowApproval {
	id := uuid.New()
	payload := ensureJSONObject(body.Payload)
	holder := map[string]any{}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &holder)
	}
	holder["workflow_id"] = body.WorkflowID
	holder["workflow_run_id"] = body.WorkflowRunID
	holder["step_id"] = body.StepID
	holder["instructions"] = body.Instructions
	merged, _ := json.Marshal(holder)

	return WorkflowApproval{
		ID:            id,
		WorkflowID:    body.WorkflowID,
		WorkflowRunID: body.WorkflowRunID,
		StepID:        body.StepID,
		Title:         body.Title,
		Instructions:  body.Instructions,
		AssignedTo:    body.AssignedTo,
		Status:        "pending",
		Payload:       merged,
		RequestedAt:   time.Now().UTC(),
	}
}

func aggregateFromProjection(approval *WorkflowApproval, body *CreateApprovalRequest, ttlHours uint32) *ApprovalRequest {
	tenantID := body.WorkflowID.String()
	if len(approval.Payload) > 0 {
		var holder map[string]any
		if err := json.Unmarshal(approval.Payload, &holder); err == nil {
			if v, ok := holder["tenant_id"].(string); ok && v != "" {
				tenantID = v
			}
		}
	}
	approverSet := []string{}
	if body.AssignedTo != nil {
		approverSet = append(approverSet, body.AssignedTo.String())
	}
	expiresAt := timeFromPayloadOrDefault(body.Payload, ttlHours)
	return New(
		approval.ID, tenantID, approval.Title, approverSet, approval.Payload,
		approval.ID, expiresAt,
	)
}

func timeFromPayloadOrDefault(payload json.RawMessage, ttlHours uint32) *time.Time {
	if len(payload) > 0 {
		var holder map[string]any
		if err := json.Unmarshal(payload, &holder); err == nil {
			if raw, ok := holder["expires_at"].(string); ok && raw != "" {
				if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
					t := parsed.UTC()
					return &t
				}
			}
		}
	}
	t := time.Now().UTC().Add(time.Duration(ttlHours) * time.Hour)
	return &t
}

func withStateMetadata(approval WorkflowApproval, aggregate *ApprovalRequest) WorkflowApproval {
	holder := map[string]any{}
	if len(approval.Payload) > 0 {
		_ = json.Unmarshal(approval.Payload, &holder)
	}
	holder["approval_state"] = map[string]any{
		"id":              aggregate.ID,
		"state":           aggregate.StateField,
		"expires_at":      aggregate.ExpiresAtField,
		"correlation_id":  aggregate.CorrelationID,
		"topic_requested": ApprovalRequestedV1,
		"topic_completed": ApprovalCompletedV1,
		"authoritative":   "audit_compliance.approval_requests",
	}
	merged, _ := json.Marshal(holder)
	approval.Payload = merged
	return approval
}

func firstApproverUUID(approvers []string) *uuid.UUID {
	if len(approvers) == 0 {
		return nil
	}
	id, err := uuid.Parse(approvers[0])
	if err != nil {
		return nil
	}
	return &id
}

func ensureJSONObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	var holder any
	if err := json.Unmarshal(raw, &holder); err != nil {
		return json.RawMessage(`{}`)
	}
	if _, ok := holder.(map[string]any); !ok {
		return json.RawMessage(`{}`)
	}
	return raw
}

func parseListQuery(r *http.Request) ListApprovalsQuery {
	q := ListApprovalsQuery{}
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.Page = &n
		}
	}
	if v := r.URL.Query().Get("per_page"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			q.PerPage = &n
		}
	}
	if v := r.URL.Query().Get("status"); v != "" {
		q.Status = &v
	}
	if v := r.URL.Query().Get("assigned_to"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			q.AssignedTo = &id
		}
	}
	if v := r.URL.Query().Get("workflow_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			q.WorkflowID = &id
		}
	}
	return q
}

func derefInt64(p *int64, fallback int64) int64 {
	if p == nil {
		return fallback
	}
	return *p
}

func clampInt64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Defence in depth — surfaces an unused-error placeholder so a future
// refactor that switches the helpers to plain `errors.New` does not
// silently drop dependency tracking.
var _ = errors.New
