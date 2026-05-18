package anomaly

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDetectorFlagsNewActorOffHoursAndBurst(t *testing.T) {
	d := NewDetector(1, time.Hour, nil)
	actor := uuid.New()
	key := uuid.New()
	d.Record(context.Background(), DecryptEvent{ActorID: actor, KeyID: key, At: time.Date(2026, 5, 18, 2, 0, 0, 0, time.UTC)})
	d.Record(context.Background(), DecryptEvent{ActorID: actor, KeyID: key, At: time.Date(2026, 5, 18, 2, 1, 0, 0, time.UTC)})
	findings := d.Findings()
	seen := map[string]bool{}
	for _, f := range findings {
		seen[f.Reason] = true
	}
	for _, reason := range []string{"new_actor", "off_hours", "sudden_burst"} {
		if !seen[reason] {
			t.Fatalf("missing %s finding: %+v", reason, findings)
		}
	}
}
