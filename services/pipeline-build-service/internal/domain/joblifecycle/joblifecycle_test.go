package joblifecycle

import (
	"testing"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

// Mirrors the Rust unit `happy_path_transitions_are_valid`.
func TestHappyPathTransitionsAreValid(t *testing.T) {
	t.Parallel()
	if !IsValidTransition(models.JobWaiting, models.JobRunPending) {
		t.Error("WAITING → RUN_PENDING should be valid")
	}
	if !IsValidTransition(models.JobRunPending, models.JobRunning) {
		t.Error("RUN_PENDING → RUNNING should be valid")
	}
	if !IsValidTransition(models.JobRunning, models.JobCompleted) {
		t.Error("RUNNING → COMPLETED should be valid")
	}
	if !IsValidTransition(models.JobRunning, models.JobFailed) {
		t.Error("RUNNING → FAILED should be valid")
	}
}

// Mirrors `abort_paths_are_valid`.
func TestAbortPathsAreValid(t *testing.T) {
	t.Parallel()
	cases := [][2]models.JobState{
		{models.JobRunning, models.JobAbortPending},
		{models.JobRunPending, models.JobAbortPending},
		{models.JobAbortPending, models.JobAborted},
		{models.JobWaiting, models.JobAborted},
		{models.JobWaiting, models.JobAbortPending},
	}
	for _, c := range cases {
		if !IsValidTransition(c[0], c[1]) {
			t.Errorf("%s → %s should be valid", c[0], c[1])
		}
	}
}

// Mirrors `skipping_states_is_rejected`.
func TestSkippingStatesIsRejected(t *testing.T) {
	t.Parallel()
	if IsValidTransition(models.JobWaiting, models.JobRunning) {
		t.Error("WAITING → RUNNING must be rejected (cannot skip RUN_PENDING)")
	}
	if IsValidTransition(models.JobCompleted, models.JobFailed) {
		t.Error("COMPLETED is terminal")
	}
	if IsValidTransition(models.JobCompleted, models.JobRunning) {
		t.Error("COMPLETED is terminal")
	}
	if IsValidTransition(models.JobFailed, models.JobRunning) {
		t.Error("FAILED is terminal")
	}
	if IsValidTransition(models.JobRunning, models.JobAborted) {
		t.Error("RUNNING → ABORTED bypasses ABORT_PENDING")
	}
}

// Mirrors `terminal_states_have_no_outgoing_edges`.
func TestTerminalStatesHaveNoOutgoingEdges(t *testing.T) {
	t.Parallel()
	for _, terminal := range []models.JobState{
		models.JobCompleted, models.JobFailed, models.JobAborted,
	} {
		for _, target := range models.AllJobStates {
			if IsValidTransition(terminal, target) {
				t.Errorf("%s → %s must be rejected", terminal, target)
			}
		}
	}
}
