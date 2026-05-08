package event

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNamespaceVerbatimFromRust(t *testing.T) {
	t.Parallel()
	expected := uuid.UUID{
		0x4e, 0x21, 0x9b, 0x1a, 0x57, 0x9c, 0x4b, 0x37,
		0xb6, 0x29, 0x6c, 0xfe, 0x6e, 0x47, 0xd1, 0x40,
	}
	assert.Equal(t, expected, WorkflowAutomationNamespace,
		"namespace bytes are part of the wire contract — DO NOT change without a fleet-wide migration")
}

func TestDeriveRunIDStableAndDistinct(t *testing.T) {
	t.Parallel()
	def := uuid.New()
	corr := uuid.New()
	a := DeriveRunID(def, corr)
	b := DeriveRunID(def, corr)
	assert.Equal(t, a, b, "stable across calls")
	assert.Equal(t, uuid.Version(5), a.Version())

	other := DeriveRunID(uuid.New(), corr)
	assert.NotEqual(t, a, other, "different definition_id → different run_id")
}

func TestDeriveConditionEventIDIsDistinctFromRunID(t *testing.T) {
	t.Parallel()
	def := uuid.New()
	corr := uuid.New()
	runID := DeriveRunID(def, corr)
	condID := DeriveConditionEventID(def, corr)
	assert.NotEqual(t, runID, condID,
		"run_id and condition_event_id must derive into separate UUID namespaces (the trailing 'C' byte)")
	// Stable across calls.
	assert.Equal(t, condID, DeriveConditionEventID(def, corr))
}

func TestTenantUUIDFromStrRoundTripsUUIDInputs(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	assert.Equal(t, id, TenantUUIDFromStr(id.String()))
}

func TestTenantUUIDFromStrCoercesNonUUID(t *testing.T) {
	t.Parallel()
	a := TenantUUIDFromStr("acme-prod")
	b := TenantUUIDFromStr("acme-prod")
	assert.Equal(t, a, b, "non-UUID slugs map deterministically via UUIDv5")
	assert.Equal(t, uuid.Version(5), a.Version())
	assert.NotEqual(t, a, TenantUUIDFromStr("acme-stage"))
}

func TestAutomateConditionV1Shape(t *testing.T) {
	t.Parallel()
	def := uuid.New()
	corr := uuid.New()
	c := AutomateConditionV1{
		DefinitionID:   def,
		TenantID:       "t-a",
		CorrelationID:  corr,
		TriggeredBy:    "alice",
		TriggerType:    "manual",
		TriggerPayload: json.RawMessage(`{"action_id":"foo"}`),
	}
	b, err := json.Marshal(c)
	require.NoError(t, err)
	var back AutomateConditionV1
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, c, back)
}

func TestAutomateOutcomeV1OmitsOptionalFields(t *testing.T) {
	t.Parallel()
	o := AutomateOutcomeV1{
		RunID:         uuid.New(),
		DefinitionID:  uuid.New(),
		TenantID:      "t-a",
		CorrelationID: uuid.New(),
		Status:        "completed",
		Attempts:      1,
	}
	b, err := json.Marshal(o)
	require.NoError(t, err)
	s := string(b)
	assert.NotContains(t, s, "effect_response", "omitempty should elide nil JSON body")
	assert.NotContains(t, s, "error", "omitempty should elide nil error")
}
