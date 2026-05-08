package automationoperations

import (
	"strings"
	"testing"
)

func TestSagaTopicsVerbatim(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"saga.step.requested.v1":   SagaStepRequestedV1,
		"saga.step.completed.v1":   SagaStepCompletedV1,
		"saga.step.failed.v1":      SagaStepFailedV1,
		"saga.step.compensated.v1": SagaStepCompensatedV1,
		"saga.compensate.v1":       SagaCompensateV1,
		"saga.completed.v1":        SagaCompletedV1,
		"saga.aborted.v1":          SagaAbortedV1,
	}
	for want, got := range cases {
		if got != want {
			t.Fatalf("saga topic mismatch: got %q want %q", got, want)
		}
	}
}

func TestEverySagaTopicEndsWithV1(t *testing.T) {
	t.Parallel()
	for _, topic := range []string{
		SagaStepRequestedV1, SagaStepCompletedV1, SagaStepFailedV1, SagaStepCompensatedV1,
		SagaCompensateV1, SagaCompletedV1, SagaAbortedV1,
	} {
		if !strings.HasSuffix(topic, ".v1") {
			t.Fatalf("topic %q must end with .v1", topic)
		}
	}
}

func TestSagaConsumerGroup(t *testing.T) {
	t.Parallel()
	if SagaConsumerGroup != "automation-operations-service" {
		t.Fatalf("SagaConsumerGroup verbatim from Rust; got %q", SagaConsumerGroup)
	}
}

func TestProcessedEventsTable(t *testing.T) {
	t.Parallel()
	if ProcessedEventsTable != "automation_operations.processed_events" {
		t.Fatalf("ProcessedEventsTable mismatch: %q", ProcessedEventsTable)
	}
}
