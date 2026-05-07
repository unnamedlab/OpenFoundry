package approvals

import (
	"strings"
	"testing"
)

func TestApprovalTopicsVerbatim(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"approval.requested.v1": ApprovalRequestedV1,
		"approval.decided.v1":   ApprovalDecidedV1,
		"approval.completed.v1": ApprovalCompletedV1,
		"approval.expired.v1":   ApprovalExpiredV1,
	}
	for want, got := range cases {
		if got != want {
			t.Fatalf("approval topic mismatch: got %q want %q", got, want)
		}
	}
}

func TestEveryApprovalTopicEndsWithV1(t *testing.T) {
	t.Parallel()
	for _, topic := range []string{
		ApprovalRequestedV1, ApprovalDecidedV1, ApprovalCompletedV1, ApprovalExpiredV1,
	} {
		if !strings.HasSuffix(topic, ".v1") {
			t.Fatalf("topic %q must end with .v1", topic)
		}
	}
}
