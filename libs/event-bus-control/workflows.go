package controlbus

// workflows.go ports libs/event-bus-control/src/workflows.rs.

import (
	"encoding/json"

	"github.com/google/uuid"
)

// WorkflowRunRequested is the payload for a "workflow.run.requested"
// event — distinct from WorkflowTriggerRequested in contracts.go: the
// trigger event arrives at the orchestrator from the user/UI and the
// run-requested event is what the orchestrator emits to start a
// concrete run.
//
// JSON shape mirrors libs/event-bus-control/src/workflows.rs::WorkflowRunRequested
// verbatim so a Rust publisher and a Go consumer round-trip the
// payload unchanged.
type WorkflowRunRequested struct {
	WorkflowID    uuid.UUID       `json:"workflow_id"`
	TriggerType   string          `json:"trigger_type"`
	StartedBy     *uuid.UUID      `json:"started_by,omitempty"`
	Context       json.RawMessage `json:"context,omitempty"`
	CorrelationID uuid.UUID       `json:"correlation_id"`
}
