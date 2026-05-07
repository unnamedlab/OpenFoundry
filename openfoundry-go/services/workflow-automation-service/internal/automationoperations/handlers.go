// HTTP handlers — every inbound producer (`POST /api/v1/automations`)
// lands here and publishes a `saga.step.requested.v1` event via the
// transactional outbox plus an `saga.state` row in the same
// Postgres transaction.

package automationoperations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/libs/outbox"
	saga "github.com/openfoundry/openfoundry-go/libs/saga"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/state"
)

// Handlers wires AppState into the automations HTTP endpoints.
type Handlers struct {
	State *state.AppState
}

// NewHandlers wires the automations handlers.
func NewHandlers(s *state.AppState) *Handlers { return &Handlers{State: s} }

// CreatePrimaryRequest mirrors the Rust struct of the same name.
type CreatePrimaryRequest struct {
	Payload json.RawMessage `json:"payload"`
}

// CreateSecondaryRequest mirrors the Rust struct of the same name.
type CreateSecondaryRequest struct {
	Payload json.RawMessage `json:"payload"`
}

// ListItems ports `automation_operations::handlers::list_items`.
func (h *Handlers) ListItems(w http.ResponseWriter, r *http.Request) {
	rows, err := h.State.DB.Query(r.Context(),
		`SELECT saga_id, name, status, current_step, updated_at
		   FROM saga.state
		  ORDER BY updated_at DESC LIMIT 100`,
	)
	if err != nil {
		slog.Error("list_items: saga_state query failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	type item struct {
		ID          uuid.UUID `json:"id"`
		Name        string    `json:"name"`
		Status      string    `json:"status"`
		CurrentStep *string   `json:"current_step"`
		UpdatedAt   time.Time `json:"updated_at"`
	}
	out := make([]item, 0)
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Name, &it.Status, &it.CurrentStep, &it.UpdatedAt); err != nil {
			slog.Error("list_items scan failed", slog.String("error", err.Error()))
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

// CreateItem ports `automation_operations::handlers::create_item`.
func (h *Handlers) CreateItem(w http.ResponseWriter, r *http.Request) {
	var body CreatePrimaryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request, err := parsePayload(body.Payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !IsKnown(request.Saga) {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("unknown saga task_type %q; known: %v", request.Saga, KnownSagaTypes))
		return
	}
	if err := dispatchRequest(r.Context(), h.State, request); err != nil {
		slog.Error("create_item dispatch failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":              request.SagaID,
		"saga":            request.Saga,
		"tenant_id":       request.TenantID,
		"correlation_id":  request.CorrelationID,
		"status":          "running",
		"created_at":      time.Now().UTC(),
		"topic":           saga.SagaStepRequestedV1,
	})
}

// GetItem ports `automation_operations::handlers::get_item`.
func (h *Handlers) GetItem(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be a uuid")
		return
	}
	row := h.State.DB.QueryRow(r.Context(),
		`SELECT saga_id, name, status, current_step, completed_steps, step_outputs,
		        failed_step, created_at, updated_at
		   FROM saga.state
		  WHERE saga_id = $1`,
		id,
	)
	var (
		sagaID         uuid.UUID
		name, status   string
		currentStep    *string
		completed      []string
		stepOutputs    []byte
		failedStep     *string
		createdAt      time.Time
		updatedAt      time.Time
	)
	if err := row.Scan(&sagaID, &name, &status, &currentStep, &completed, &stepOutputs, &failedStep, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		slog.Error("get_item: saga_state query failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":               sagaID,
		"name":             name,
		"status":           status,
		"current_step":     currentStep,
		"completed_steps":  completed,
		"step_outputs":     json.RawMessage(stepOutputs),
		"failed_step":      failedStep,
		"created_at":       createdAt,
		"updated_at":       updatedAt,
	})
}

// ListSecondary ports `automation_operations::handlers::list_secondary`.
func (h *Handlers) ListSecondary(w http.ResponseWriter, r *http.Request) {
	parentID, err := uuid.Parse(chi.URLParam(r, "parent_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "parent_id must be a uuid")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":      []any{},
		"parent_id": parentID,
		"note":      "step history is rolled into the parent saga's step_outputs JSON; query GET /automations/{id} for the full timeline",
	})
}

// CreateSecondary ports `automation_operations::handlers::create_secondary`.
//
// Returns 410 GONE — manual recording of arbitrary steps is not
// supported under the FASE 6 saga model.
func (h *Handlers) CreateSecondary(w http.ResponseWriter, r *http.Request) {
	parentID, err := uuid.Parse(chi.URLParam(r, "parent_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "parent_id must be a uuid")
		return
	}
	writeJSON(w, http.StatusGone, map[string]any{
		"error":     "manual recording of automation runs is not supported; use POST /api/v1/automations to start a saga",
		"parent_id": parentID,
	})
}

// parsePayload ports `parse_payload`. Pure, no IO. Unit-testable.
func parsePayload(raw json.RawMessage) (saga.SagaStepRequestedV1Payload, error) {
	var holder map[string]json.RawMessage
	if err := json.Unmarshal(rawOrEmptyObject(raw), &holder); err != nil {
		return saga.SagaStepRequestedV1Payload{}, fmt.Errorf("payload must be a JSON object: %w", err)
	}

	taskType, ok := readJSONString(holder, "task_type")
	if !ok || taskType == "" {
		return saga.SagaStepRequestedV1Payload{}, errors.New("payload.task_type is required")
	}
	tenantID, _ := readJSONString(holder, "tenant_id")
	if tenantID == "" {
		tenantID = "default"
	}
	triggeredBy, _ := readJSONString(holder, "triggered_by")
	if triggeredBy == "" {
		triggeredBy = "system"
	}

	correlationID, err := readUUID(holder, "audit_correlation_id", "correlation_id")
	if err != nil {
		return saga.SagaStepRequestedV1Payload{}, err
	}
	if correlationID == uuid.Nil {
		correlationID = uuid.New()
	}

	sagaID, err := readUUID(holder, "task_id")
	if err != nil {
		return saga.SagaStepRequestedV1Payload{}, fmt.Errorf("invalid task_id: %w", err)
	}
	if sagaID == uuid.Nil {
		sagaID = DeriveSagaID(taskType, correlationID)
	}

	input := holder["input"]
	if len(input) == 0 {
		input = holder["payload"]
	}
	if len(input) == 0 {
		input = json.RawMessage("null")
	}

	return saga.SagaStepRequestedV1Payload{
		SagaID:        sagaID,
		Saga:          taskType,
		TenantID:      tenantID,
		CorrelationID: correlationID,
		TriggeredBy:   triggeredBy,
		Input:         input,
	}, nil
}

// dispatchRequest INSERTs the saga_state row + the outbox event in a
// single transaction.
func dispatchRequest(ctx context.Context, s *state.AppState, request saga.SagaStepRequestedV1Payload) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`INSERT INTO saga.state (saga_id, name)
		      VALUES ($1, $2)
		    ON CONFLICT (saga_id) DO NOTHING`,
		request.SagaID, request.Saga,
	); err != nil {
		return fmt.Errorf("saga_state insert failed: %w", err)
	}

	eventID := DeriveRequestEventID(request.Saga, request.CorrelationID)
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	out := outbox.New(eventID, "saga", request.SagaID.String(), saga.SagaStepRequestedV1, body).
		WithHeader("x-audit-correlation-id", request.CorrelationID.String()).
		WithHeader("ol-job", fmt.Sprintf("saga/%s", request.Saga)).
		WithHeader("ol-run-id", request.SagaID.String()).
		WithHeader("ol-producer", "automation-operations-service")
	if err := outbox.Enqueue(ctx, tx, out); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func rawOrEmptyObject(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("{}")
	}
	return []byte(raw)
}

func readJSONString(holder map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := holder[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func readUUID(holder map[string]json.RawMessage, keys ...string) (uuid.UUID, error) {
	for _, key := range keys {
		raw, ok := holder[key]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return uuid.Nil, fmt.Errorf("invalid %s: %w", key, err)
		}
		if s == "" {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil {
			return uuid.Nil, fmt.Errorf("invalid %s: %w", key, err)
		}
		return id, nil
	}
	return uuid.Nil, nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
