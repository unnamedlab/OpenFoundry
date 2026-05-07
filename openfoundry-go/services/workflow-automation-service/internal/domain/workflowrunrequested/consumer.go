// Package workflowrunrequested ports
// `services/workflow-automation-service/src/domain/workflow_run_requested.rs`
// 1:1.
//
// Legacy NATS consumer for `of.workflows.run.requested`. Kept until
// `pipeline-schedule-service` is migrated to publish directly to
// `automate.condition.v1`. Each inbound event is forwarded into the
// `start_internal_triggered_run` execute path.
package workflowrunrequested

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	controlbus "github.com/openfoundry/openfoundry-go/libs/event-bus-control"
	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/models"
)

// ConsumerName is the durable JetStream consumer name. Pinned so a
// rolling restart does not fork the consumer state.
const ConsumerName = "workflow-automation-run-requested"

// WorkflowRunRequested mirrors the Rust `WorkflowRunRequested` payload
// the legacy NATS topic carries. The shared Go `controlbus.WorkflowTriggerRequested`
// only has a subset of fields; this struct carries the full payload
// the consumer dispatches off of.
type WorkflowRunRequested struct {
	WorkflowID    uuid.UUID       `json:"workflow_id"`
	TriggerType   string          `json:"trigger_type"`
	StartedBy     *uuid.UUID      `json:"started_by,omitempty"`
	Context       json.RawMessage `json:"context,omitempty"`
	CorrelationID uuid.UUID       `json:"correlation_id"`
}

// Event mirrors the Rust `event_bus_control::schemas::Event<T>` envelope.
// The publisher wraps the payload in `{ id, kind, payload, ... }`.
type Event[T any] struct {
	ID            uuid.UUID `json:"id"`
	Kind          string    `json:"kind"`
	OccurredAt    string    `json:"occurred_at,omitempty"`
	CorrelationID uuid.UUID `json:"correlation_id,omitempty"`
	Payload       T         `json:"payload"`
}

// Dispatcher is the per-message handler the loop calls on every
// successfully-decoded run-requested event.
type Dispatcher func(ctx context.Context, workflowID uuid.UUID, request models.InternalTriggeredRunRequest) (*models.WorkflowRun, error)

// Consume mirrors `domain::workflow_run_requested::consume`.
//
// Connects to NATS, ensures the OF_EVENTS stream + the durable
// consumer, and pulls messages indefinitely. Each successfully
// decoded event is forwarded to `dispatch`.
func Consume(ctx context.Context, natsURL string, dispatch Dispatcher) error {
	conn, err := nats.Connect(natsURL)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	defer conn.Drain() //nolint:errcheck // best-effort close on graceful shutdown
	js, err := jetstream.New(conn)
	if err != nil {
		return fmt.Errorf("failed to create JetStream context: %w", err)
	}

	stream, err := controlbus.EnsureStream(ctx, js, controlbus.StreamEvents, []string{controlbus.SubjectWorkflows})
	if err != nil {
		return fmt.Errorf("failed to ensure workflow event stream: %w", err)
	}
	subject := controlbus.SubjectWorkflows + ".run.requested"
	cons, err := controlbus.CreateConsumer(ctx, stream, ConsumerName, subject)
	if err != nil {
		return fmt.Errorf("failed to create workflow run consumer: %w", err)
	}

	iter, err := cons.Messages()
	if err != nil {
		return fmt.Errorf("failed to open workflow run message stream: %w", err)
	}
	defer iter.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		msg, err := iter.Next()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			slog.Warn("workflow run consumer message read failed",
				slog.String("error", err.Error()))
			continue
		}
		var ev Event[WorkflowRunRequested]
		if err := json.Unmarshal(msg.Data(), &ev); err != nil {
			slog.Warn("workflow run consumer payload decode failed",
				slog.String("error", err.Error()))
			continue
		}
		req := models.InternalTriggeredRunRequest{
			WorkflowID:    ev.Payload.WorkflowID,
			TriggerType:   ev.Payload.TriggerType,
			StartedBy:     ev.Payload.StartedBy,
			Context:       ev.Payload.Context,
			CorrelationID: ev.Payload.CorrelationID,
		}
		run, err := dispatch(ctx, ev.Payload.WorkflowID, req)
		if err != nil {
			slog.Warn("workflow.run.requested failed",
				slog.String("workflow_id", ev.Payload.WorkflowID.String()),
				slog.String("correlation_id", ev.Payload.CorrelationID.String()),
				slog.String("error", err.Error()))
			continue
		}
		if err := msg.Ack(); err != nil {
			return fmt.Errorf("failed to ack workflow.run.requested: %w", err)
		}
		slog.Info("workflow.run.requested accepted",
			slog.String("workflow_id", run.WorkflowID.String()),
			slog.String("run_id", run.ID.String()),
			slog.String("correlation_id", ev.Payload.CorrelationID.String()))
	}
}
