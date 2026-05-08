// Unit tests for the cron-driven scheduler. Mirrors the
// `#[cfg(test)] mod tests { ... }` block in
// libs/event-scheduler/src/lib.rs 1:1 — same case names, same
// fixture, same assertions.
package eventscheduler_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	es "github.com/openfoundry/openfoundry-go/libs/event-scheduler"
)

func def(cronExpr string) es.ScheduleDefinition {
	return es.ScheduleDefinition{
		ID:              uuid.Nil,
		Name:            "demo",
		CronExpr:        cronExpr,
		CronFlavor:      "unix5",
		TimeZone:        "UTC",
		Enabled:         true,
		Topic:           "of.schedules.demo",
		PayloadTemplate: json.RawMessage(`{"hello":"world"}`),
		NextRunAt:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestComputeNextFireAdvancesMinuteCron(t *testing.T) {
	t.Parallel()
	d := def("*/5 * * * *") // every 5 minutes
	after := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	got, err := es.ComputeNextFire(&d, after)
	if err != nil {
		t.Fatalf("compute_next_fire: %v", err)
	}
	want := time.Date(2026, 5, 4, 12, 5, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestComputeNextFireRespectsQuartz6SecondsField(t *testing.T) {
	t.Parallel()
	d := def("0 * * * * *")
	d.CronFlavor = "quartz6"
	after := time.Date(2026, 5, 4, 12, 0, 30, 0, time.UTC)
	got, err := es.ComputeNextFire(&d, after)
	if err != nil {
		t.Fatalf("compute_next_fire: %v", err)
	}
	want := time.Date(2026, 5, 4, 12, 1, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestComputeNextFireUsesIANATimeZone(t *testing.T) {
	t.Parallel()
	// Daily at 09:00 New York time. After 2026-05-04 12:00 UTC
	// (= 08:00 EDT), next fire is 13:00 UTC (= 09:00 EDT) the
	// same day.
	d := def("0 9 * * *")
	d.TimeZone = "America/New_York"
	after := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	got, err := es.ComputeNextFire(&d, after)
	if err != nil {
		t.Fatalf("compute_next_fire: %v", err)
	}
	want := time.Date(2026, 5, 4, 13, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestComputeNextFireRejectsUnknownFlavor(t *testing.T) {
	t.Parallel()
	d := def("*/5 * * * *")
	d.CronFlavor = "garbage"
	_, err := es.ComputeNextFire(&d, time.Now())
	if err == nil {
		t.Fatal("must reject")
	}
	var se *es.SchedulerError
	if !errors.As(err, &se) || se.Kind != es.ErrUnknownFlavor {
		t.Fatalf("got %+v, want ErrUnknownFlavor", err)
	}
}

func TestComputeNextFireRejectsInvalidTimeZone(t *testing.T) {
	t.Parallel()
	d := def("*/5 * * * *")
	d.TimeZone = "Mars/Olympus"
	_, err := es.ComputeNextFire(&d, time.Now())
	if err == nil {
		t.Fatal("must reject")
	}
	var se *es.SchedulerError
	if !errors.As(err, &se) || se.Kind != es.ErrInvalidTimeZone {
		t.Fatalf("got %+v, want ErrInvalidTimeZone", err)
	}
}

func TestComputeNextFireRejectsInvalidCronExpr(t *testing.T) {
	t.Parallel()
	d := def("not a cron expression")
	_, err := es.ComputeNextFire(&d, time.Now())
	if err == nil {
		t.Fatal("must reject")
	}
	var se *es.SchedulerError
	if !errors.As(err, &se) || se.Kind != es.ErrInvalidCron {
		t.Fatalf("got %+v, want ErrInvalidCron", err)
	}
}

func TestBuildLineageRunIDIsDeterministicPerScheduledFor(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	a := es.BuildLineage("nightly-rollup", when)
	b := es.BuildLineage("nightly-rollup", when)
	if a.RunID != b.RunID {
		t.Fatalf("run_id is not deterministic: %s vs %s", a.RunID, b.RunID)
	}
	if a.Namespace != es.LineageNamespace {
		t.Fatalf("namespace = %s, want %s", a.Namespace, es.LineageNamespace)
	}
	if a.JobName != "nightly-rollup" {
		t.Fatalf("job_name = %s, want nightly-rollup", a.JobName)
	}
	if !a.EventTime.Equal(when) {
		t.Fatalf("event_time = %s, want %s", a.EventTime, when)
	}

	// Different scheduled_for ⇒ different run_id.
	later := when.Add(5 * time.Minute)
	c := es.BuildLineage("nightly-rollup", later)
	if a.RunID == c.RunID {
		t.Fatalf("run_id collision across scheduled_for: both %s", a.RunID)
	}
}

// TestBuildLineageRunIDMatchesRustReference locks the Go run_id to
// the Rust runtime's byte-for-byte. Drifts here would break consumer
// dedup across mixed-runtime fleets.
//
// Reference value computed with `uuid::Uuid::new_v5(&Uuid::NAMESPACE_OID, …)`
// on the chrono `to_rfc3339()` form of the input — same input as
// `uuid.uuid5(NAMESPACE_OID, "nightly-rollup|2026-05-04T12:00:00+00:00")`.
func TestBuildLineageRunIDMatchesRustReference(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	got := es.BuildLineage("nightly-rollup", when)
	const want = "b06d93c0-f1ca-5385-aacb-0ddffc072046"
	if got.RunID != want {
		t.Fatalf("run_id drifted from Rust runtime: got %s, want %s", got.RunID, want)
	}
}

func TestLineageConstantsAreStableWireFormat(t *testing.T) {
	t.Parallel()
	// These show up on the wire; locking them in.
	if es.LineageNamespace != "of://schedules" {
		t.Fatalf("namespace drifted: %q", es.LineageNamespace)
	}
	if got := es.LineageProducer; len(got) < 8 || got[:8] != "https://" {
		t.Fatalf("producer must be an https:// URI; got %q", got)
	}
}
