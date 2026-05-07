package topics

import "testing"

func TestTopicsMatchHelmProvisioning(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"automate.condition.v1": AutomateConditionV1,
		"automate.outcome.v1":   AutomateOutcomeV1,
	}
	for want, got := range cases {
		if got != want {
			t.Fatalf("topic mismatch: got %q want %q", got, want)
		}
	}
}
