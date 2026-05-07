package saga

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Topic taxonomy (mirrors Rust #[test] cases) ------------------------

func TestTopicConstantsUseV1Suffix(t *testing.T) {
	t.Parallel()
	for _, topic := range []string{
		SagaStepRequestedV1,
		SagaStepCompletedV1Topic,
		SagaStepFailedV1Topic,
		SagaStepCompensatedV1Topic,
		SagaCompensateV1,
		SagaCompletedV1Topic,
		SagaAbortedV1Topic,
	} {
		assert.True(t, strings.HasSuffix(topic, ".v1"), "topic %s must end with .v1", topic)
	}
}

func TestSagaEventKindTopicsAreStable(t *testing.T) {
	t.Parallel()
	// These strings show up on the wire — locking them in.
	assert.Equal(t, "saga.step.completed.v1", EventStepCompleted.Topic())
	assert.Equal(t, "saga.step.failed.v1", EventStepFailed.Topic())
	assert.Equal(t, "saga.step.compensated.v1", EventStepCompensated.Topic())
	assert.Equal(t, "saga.completed.v1", EventSagaCompleted.Topic())
	assert.Equal(t, "saga.aborted.v1", EventSagaAborted.Topic())
}

func TestTopicConstantsMatchRunnerEmitTopics(t *testing.T) {
	t.Parallel()
	// Defence in depth: SagaEventKind.Topic() must match the
	// constants exactly. The runner uses the enum, helm uses the
	// constants — they must agree.
	assert.Equal(t, SagaStepCompletedV1Topic, EventStepCompleted.Topic())
	assert.Equal(t, SagaStepFailedV1Topic, EventStepFailed.Topic())
	assert.Equal(t, SagaStepCompensatedV1Topic, EventStepCompensated.Topic())
	assert.Equal(t, SagaCompletedV1Topic, EventSagaCompleted.Topic())
	assert.Equal(t, SagaAbortedV1Topic, EventSagaAborted.Topic())
}

// --- Status round-trip --------------------------------------------------

func TestSagaStatusRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []SagaStatus{StatusRunning, StatusCompleted, StatusFailed, StatusCompensated, StatusAborted} {
		assert.Equal(t, s, ParseSagaStatus(string(s)))
	}
	// Unknown strings collapse to Running so an old row left
	// behind by a buggy writer can still be resumed.
	assert.Equal(t, StatusRunning, ParseSagaStatus("garbage"))
	assert.False(t, StatusRunning.IsTerminal())
	assert.True(t, StatusCompleted.IsTerminal())
	assert.True(t, StatusAborted.IsTerminal())
}

// --- Error constructors -------------------------------------------------

func TestSagaErrorConstructors(t *testing.T) {
	t.Parallel()
	e := StepFailure("reserve", "out of stock")
	assert.True(t, IsStepFailure(e))
	assert.Contains(t, e.Error(), "reserve")
	assert.Contains(t, e.Error(), "out of stock")

	c := CompensationFailure("charge", "refund failed")
	assert.True(t, IsCompensationFailure(c))
	assert.Contains(t, c.Error(), "charge")
}

func TestSagaErrorClassificationUnwraps(t *testing.T) {
	t.Parallel()
	// Wrap a step-failure inside another error and verify the
	// helper still classifies it correctly via errors.As.
	wrapped := errors.Join(errors.New("transport"), StepFailure("x", "y"))
	assert.True(t, IsStepFailure(wrapped))
	assert.False(t, IsCompensationFailure(wrapped))
}

// --- Event payloads round-trip (mirrors Rust #[test] cases) -------------

func TestStepCompletedV1RoundTrip(t *testing.T) {
	t.Parallel()
	event := SagaStepCompletedV1{
		SagaID: uuid.Nil,
		Saga:   "retention.sweep",
		Step:   "evict_old_objects",
		Output: json.RawMessage(`{"evicted":42}`),
	}
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	var back SagaStepCompletedV1
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, event.SagaID, back.SagaID)
	assert.Equal(t, event.Saga, back.Saga)
	assert.Equal(t, event.Step, back.Step)
	assert.JSONEq(t, string(event.Output), string(back.Output))
}

func TestStepFailedV1RoundTrip(t *testing.T) {
	t.Parallel()
	event := SagaStepFailedV1{
		SagaID: uuid.Nil,
		Saga:   "cleanup.workspace",
		Step:   "drop_blobs",
		Error:  "S3 returned 503",
	}
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	var back SagaStepFailedV1
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, event, back)
}

func TestStepCompensatedV1RoundTrip(t *testing.T) {
	t.Parallel()
	event := SagaStepCompensatedV1{
		SagaID: uuid.Nil,
		Saga:   "retention.sweep",
		Step:   "evict_old_objects",
	}
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	var back SagaStepCompensatedV1
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, event, back)
}

func TestStepRequestedV1RoundTripWithInput(t *testing.T) {
	t.Parallel()
	event := SagaStepRequestedV1Payload{
		SagaID:        uuid.Nil,
		Saga:          "retention.sweep",
		TenantID:      "acme",
		CorrelationID: uuid.Nil,
		TriggeredBy:   "system",
		Input:         json.RawMessage(`{"older_than_days":90}`),
	}
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	var back SagaStepRequestedV1Payload
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, event.SagaID, back.SagaID)
	assert.Equal(t, event.Saga, back.Saga)
	assert.JSONEq(t, string(event.Input), string(back.Input))
}

func TestStepRequestedV1RoundTripWithDefaultInput(t *testing.T) {
	t.Parallel()
	// input is omitempty — when absent on the wire it round-trips
	// as nil json.RawMessage (zero-value, matches Rust serde
	// #[serde(default)] semantics).
	raw := `{
        "saga_id": "00000000-0000-0000-0000-000000000000",
        "saga": "retention.sweep",
        "tenant_id": "acme",
        "correlation_id": "00000000-0000-0000-0000-000000000000",
        "triggered_by": "system"
    }`
	var parsed SagaStepRequestedV1Payload
	require.NoError(t, json.Unmarshal([]byte(raw), &parsed))
	assert.Empty(t, parsed.Input)
}

func TestCompensateV1RoundTrip(t *testing.T) {
	t.Parallel()
	event := SagaCompensateRequestedV1{
		SagaID:        uuid.Nil,
		Saga:          "create_order",
		Reason:        "downstream payment provider rejected",
		CorrelationID: uuid.Nil,
	}
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	var back SagaCompensateRequestedV1
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, event, back)
}

func TestCompletedV1RoundTrip(t *testing.T) {
	t.Parallel()
	event := SagaCompletedV1{
		SagaID:         uuid.Nil,
		Saga:           "create_order",
		CompletedSteps: []string{"reserve_inventory", "charge_card", "ship_order"},
	}
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	var back SagaCompletedV1
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, event, back)
}

func TestAbortedV1RoundTrip(t *testing.T) {
	t.Parallel()
	event := SagaAbortedV1{SagaID: uuid.Nil, Saga: "create_order"}
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	var back SagaAbortedV1
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, event, back)
}

// --- SagaStep contract --------------------------------------------------

type reserveInput struct {
	SKU string `json:"sku"`
	Qty uint32 `json:"qty"`
}

type reserveOutput struct {
	ReservationID uuid.UUID `json:"reservation_id"`
}

// reserveStep is a mock step that compiles against SagaStep[I, O].
type reserveStep struct{}

func (reserveStep) StepName() string { return "reserve_inventory" }
func (reserveStep) Execute(_ context.Context, in reserveInput) (reserveOutput, error) {
	return reserveOutput{ReservationID: uuid.Nil}, nil
}
func (reserveStep) Compensate(_ context.Context, _ reserveInput) error { return nil }

func TestSagaStepInterfaceCompiles(t *testing.T) {
	t.Parallel()
	// Compile-time assertion that reserveStep satisfies the
	// generic SagaStep[reserveInput, reserveOutput] contract.
	var _ SagaStep[reserveInput, reserveOutput] = reserveStep{}
}
