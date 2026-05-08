// Tests for Phase D scheduling — trigger evaluation (time / event /
// compound), cron window planning, auto-pause supervisor, JSON
// round-trip of the tagged-union triggers.
package schedule

import (
	"encoding/json"
	"testing"
	"time"
)

// ── Cron windows ────────────────────────────────────────────────────

func TestBuildScheduleWindowsRejectsInvertedRange(t *testing.T) {
	t.Parallel()
	end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	start := end.Add(time.Hour)
	if _, err := BuildScheduleWindows("0 * * * *", start, end, 10, CronFlavorUnix5, time.UTC); err == nil {
		t.Error("expected inverted-range error")
	}
}

func TestBuildScheduleWindowsTopOfHour(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	got, err := BuildScheduleWindows("0 * * * *", start, end, 10, CronFlavorUnix5, time.UTC)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Cursor seed = start - 1s mirrors the Rust impl, so the first
	// "after" occurrence is exactly start_at when it lands on the
	// minute boundary. Expect 4 occurrences: 00:00, 01:00, 02:00, 03:00.
	if len(got) != 4 {
		t.Fatalf("expected 4 windows, got %d: %+v", len(got), got)
	}
	if got[0].ScheduledFor.Hour() != 0 {
		t.Errorf("first window must be 00:00 (boundary inclusion), got %v", got[0].ScheduledFor)
	}
	for i := 1; i < len(got); i++ {
		if got[i].ScheduledFor.Before(got[i-1].ScheduledFor) {
			t.Errorf("windows must be ascending, got %v", got)
		}
	}
}

func TestBuildScheduleWindowsWindowEndChainsToNextOccurrence(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Hour)
	got, _ := BuildScheduleWindows("0 * * * *", start, end, 10, CronFlavorUnix5, time.UTC)
	if len(got) < 2 {
		t.Skip("not enough windows for chain assertion")
	}
	if got[0].WindowEnd != got[1].ScheduledFor {
		t.Errorf("window end of slot N must equal scheduled time of slot N+1, got %v vs %v",
			got[0].WindowEnd, got[1].ScheduledFor)
	}
}

func TestBuildScheduleWindowsRejectsInvalidCron(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC()
	if _, err := BuildScheduleWindows("not-a-cron", start, start.Add(time.Hour), 10, CronFlavorUnix5, time.UTC); err == nil {
		t.Error("expected parse error")
	}
}

func TestClampLimit(t *testing.T) {
	t.Parallel()
	if got := ClampLimit(nil); got != DefaultBackfillLimit {
		t.Errorf("nil: got %d", got)
	}
	negative := -1
	if got := ClampLimit(&negative); got != 1 {
		t.Errorf("negative: got %d", got)
	}
	huge := 1000
	if got := ClampLimit(&huge); got != MaxBackfillLimit {
		t.Errorf("huge: got %d", got)
	}
}

// ── Trigger JSON round-trip ────────────────────────────────────────

func TestTriggerJSONRoundTripTime(t *testing.T) {
	t.Parallel()
	original := Trigger{
		Kind: TriggerKindTime,
		Time: &TimeTrigger{Cron: "0 9 * * MON-FRI", TimeZone: "America/New_York", Flavor: CronFlavorUnix5},
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Trigger
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Kind != TriggerKindTime || decoded.Time == nil {
		t.Fatalf("kind drift: %+v", decoded)
	}
	if decoded.Time.Cron != original.Time.Cron || decoded.Time.TimeZone != original.Time.TimeZone {
		t.Errorf("payload drift: %+v vs %+v", decoded.Time, original.Time)
	}
}

func TestTriggerJSONRoundTripEvent(t *testing.T) {
	t.Parallel()
	original := Trigger{
		Kind: TriggerKindEvent,
		Event: &EventTrigger{
			EventType:    EventTypeDataUpdated,
			TargetRID:    "ri.foundry.main.dataset.upstream",
			BranchFilter: []string{"master"},
		},
	}
	encoded, _ := json.Marshal(original)
	var decoded Trigger
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Event == nil || decoded.Event.EventType != EventTypeDataUpdated {
		t.Errorf("event payload drift: %+v", decoded.Event)
	}
}

func TestTriggerUnmarshalDefaultsTimeZoneToUTC(t *testing.T) {
	t.Parallel()
	raw := `{"kind":"time","data":{"cron":"0 * * * *","flavor":"UNIX5"}}`
	var decoded Trigger
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("err: %v", err)
	}
	if decoded.Time.TimeZone != "UTC" {
		t.Errorf("default TZ must be UTC, got %q", decoded.Time.TimeZone)
	}
}

// ── Trigger evaluation ─────────────────────────────────────────────

func TestEvaluateTriggerTimeFiresWhenCronMatches(t *testing.T) {
	t.Parallel()
	trigger := &Trigger{
		Kind: TriggerKindTime,
		Time: &TimeTrigger{Cron: "0 9 * * *", TimeZone: "UTC", Flavor: CronFlavorUnix5},
	}
	lastRun := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	fires, next, err := EvaluateTrigger(trigger, EvaluationContext{Now: now, LastRunAt: &lastRun})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !fires {
		t.Error("trigger must fire when cron occurrence sits between last_run and now")
	}
	if next == nil || next.Hour() != 9 {
		t.Errorf("next fire must be at 09:00, got %v", next)
	}
}

func TestEvaluateTriggerTimeDoesNotFireBeforeOccurrence(t *testing.T) {
	t.Parallel()
	trigger := &Trigger{
		Kind: TriggerKindTime,
		Time: &TimeTrigger{Cron: "0 9 * * *", TimeZone: "UTC", Flavor: CronFlavorUnix5},
	}
	lastRun := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	now := time.Date(2026, 1, 1, 8, 30, 0, 0, time.UTC)
	fires, _, err := EvaluateTrigger(trigger, EvaluationContext{Now: now, LastRunAt: &lastRun})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if fires {
		t.Error("trigger must not fire when next occurrence is in the future")
	}
}

func TestEvaluateTriggerEventMatchesTargetAndBranch(t *testing.T) {
	t.Parallel()
	trigger := &Trigger{
		Kind: TriggerKindEvent,
		Event: &EventTrigger{
			EventType:    EventTypeDataUpdated,
			TargetRID:    "ri.foundry.main.dataset.x",
			BranchFilter: []string{"master"},
		},
	}
	ctx := EvaluationContext{Event: &TriggerEvent{
		EventType: EventTypeDataUpdated, TargetRID: "ri.foundry.main.dataset.x", Branch: "master",
	}}
	fires, _, err := EvaluateTrigger(trigger, ctx)
	if err != nil || !fires {
		t.Errorf("expected match, fires=%v err=%v", fires, err)
	}
}

func TestEvaluateTriggerEventRejectsWrongBranch(t *testing.T) {
	t.Parallel()
	trigger := &Trigger{
		Kind: TriggerKindEvent,
		Event: &EventTrigger{
			EventType:    EventTypeDataUpdated,
			TargetRID:    "ri.foundry.main.dataset.x",
			BranchFilter: []string{"master"},
		},
	}
	ctx := EvaluationContext{Event: &TriggerEvent{
		EventType: EventTypeDataUpdated, TargetRID: "ri.foundry.main.dataset.x", Branch: "feature/x",
	}}
	fires, _, _ := EvaluateTrigger(trigger, ctx)
	if fires {
		t.Error("branch filter must reject unmatched branches")
	}
}

func TestEvaluateTriggerEventEmptyFilterAllowsAnyBranch(t *testing.T) {
	t.Parallel()
	trigger := &Trigger{
		Kind:  TriggerKindEvent,
		Event: &EventTrigger{EventType: EventTypeDataUpdated, TargetRID: "rid"},
	}
	ctx := EvaluationContext{Event: &TriggerEvent{
		EventType: EventTypeDataUpdated, TargetRID: "rid", Branch: "any",
	}}
	fires, _, _ := EvaluateTrigger(trigger, ctx)
	if !fires {
		t.Error("empty branch filter must accept any branch")
	}
}

func TestEvaluateTriggerCompoundOrFiresWhenAnyComponentFires(t *testing.T) {
	t.Parallel()
	trigger := &Trigger{
		Kind: TriggerKindCompound,
		Compound: &CompoundTrigger{
			Op: CompoundOpOr,
			Components: []Trigger{
				{Kind: TriggerKindTime, Time: &TimeTrigger{Cron: "0 9 * * *", TimeZone: "UTC", Flavor: CronFlavorUnix5}},
				{Kind: TriggerKindEvent, Event: &EventTrigger{EventType: EventTypeDataUpdated, TargetRID: "rid"}},
			},
		},
	}
	// Time component does not fire (now < cron occurrence), but
	// event does.
	now := time.Date(2026, 1, 1, 5, 0, 0, 0, time.UTC)
	last := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := EvaluationContext{
		Now:       now,
		LastRunAt: &last,
		Event:     &TriggerEvent{EventType: EventTypeDataUpdated, TargetRID: "rid"},
	}
	fires, _, _ := EvaluateTrigger(trigger, ctx)
	if !fires {
		t.Error("OR compound must fire when event component fires")
	}
}

func TestEvaluateTriggerCompoundAndRequiresAllComponents(t *testing.T) {
	t.Parallel()
	trigger := &Trigger{
		Kind: TriggerKindCompound,
		Compound: &CompoundTrigger{
			Op: CompoundOpAnd,
			Components: []Trigger{
				{Kind: TriggerKindTime, Time: &TimeTrigger{Cron: "0 9 * * *", TimeZone: "UTC", Flavor: CronFlavorUnix5}},
				{Kind: TriggerKindEvent, Event: &EventTrigger{EventType: EventTypeDataUpdated, TargetRID: "rid"}},
			},
		},
	}
	// Both components must fire — time fires (now > 09:00 with last run at 08:00) and event fires.
	last := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	ctx := EvaluationContext{
		Now:       now,
		LastRunAt: &last,
		Event:     &TriggerEvent{EventType: EventTypeDataUpdated, TargetRID: "rid"},
	}
	fires, _, _ := EvaluateTrigger(trigger, ctx)
	if !fires {
		t.Error("AND compound must fire when all components fire")
	}
}

func TestEvaluateTriggerCompoundAndRejectsWhenAnyComponentSkips(t *testing.T) {
	t.Parallel()
	trigger := &Trigger{
		Kind: TriggerKindCompound,
		Compound: &CompoundTrigger{
			Op: CompoundOpAnd,
			Components: []Trigger{
				{Kind: TriggerKindTime, Time: &TimeTrigger{Cron: "0 9 * * *", TimeZone: "UTC", Flavor: CronFlavorUnix5}},
				{Kind: TriggerKindEvent, Event: &EventTrigger{EventType: EventTypeDataUpdated, TargetRID: "rid"}},
			},
		},
	}
	// Only the time component fires; event is missing.
	last := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	ctx := EvaluationContext{Now: now, LastRunAt: &last}
	fires, _, _ := EvaluateTrigger(trigger, ctx)
	if fires {
		t.Error("AND compound must not fire when any component skips")
	}
}

// ── Auto-pause supervisor ──────────────────────────────────────────

func TestShouldAutoPauseAfterThresholdConsecutiveFailures(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	history := make([]RunOutcome, AutoPauseFailureThreshold)
	for i := range history {
		history[i] = RunOutcome{Succeeded: false, OccurredAt: now}
	}
	if !ShouldAutoPause(history) {
		t.Error("expected auto-pause after threshold failures")
	}
}

func TestShouldAutoPauseResetsOnSuccess(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	history := []RunOutcome{
		{Succeeded: false, OccurredAt: now},
		{Succeeded: false, OccurredAt: now},
		{Succeeded: true, OccurredAt: now}, // streak reset
		{Succeeded: false, OccurredAt: now},
		{Succeeded: false, OccurredAt: now},
	}
	if ShouldAutoPause(history) {
		t.Error("a recent success must reset the failure streak")
	}
}

func TestShouldAutoPauseBelowThreshold(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	history := []RunOutcome{
		{Succeeded: false, OccurredAt: now},
		{Succeeded: false, OccurredAt: now},
	}
	if ShouldAutoPause(history) {
		t.Error("must not auto-pause below threshold")
	}
}

func TestAutoPauseExemptHonoursMarker(t *testing.T) {
	t.Parallel()
	exempt := &Schedule{Description: "production schedule, AUTO_PAUSE_EXEMPT due to SLA"}
	if !AutoPauseExempt(exempt) {
		t.Error("schedule with marker must be exempt")
	}
	regular := &Schedule{Description: "regular schedule"}
	if AutoPauseExempt(regular) {
		t.Error("schedule without marker must NOT be exempt")
	}
}
