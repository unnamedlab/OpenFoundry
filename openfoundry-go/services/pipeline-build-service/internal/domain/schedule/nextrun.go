// Legacy `pipelines.next_run_at` recompute helper.
//
// 1:1 port of `services/pipeline-build-service/src/domain/executor.rs`
// `compute_next_run_at_from_parts`. The legacy run-due scheduler
// updates `pipelines.next_run_at` after each scheduled trigger; the
// Rust impl reads the cron expression from the pipeline's
// `schedule_config` JSON, parses it as a 5-field Unix expression in
// UTC, and asks for the next upcoming fire time.
//
// The Go port lives next to the existing trigger evaluator so it
// reuses the libs/scheduling-cron parser and stays consistent with
// `EvaluateTrigger` (which the new schedules path uses). When the
// pipeline is paused, the schedule is disabled, the cron field is
// missing, or parsing fails, the function returns nil — matching the
// Rust `Option<DateTime<Utc>>` return shape.

package schedule

import (
	"strings"
	"time"

	cron "github.com/openfoundry/openfoundry-go/libs/scheduling-cron"
)

// ComputeNextRunAt mirrors `pub fn compute_next_run_at_from_parts`.
// `now` is injected so callers can plug in a deterministic clock in
// tests; production code passes `time.Now().UTC()`.
func ComputeNextRunAt(status string, enabled bool, cronExpr *string, now time.Time) *time.Time {
	if status != "active" || !enabled || cronExpr == nil {
		return nil
	}
	expr := strings.TrimSpace(*cronExpr)
	if expr == "" {
		return nil
	}
	parsed, err := cron.ParseCron(expr, cron.Unix5, time.UTC)
	if err != nil {
		return nil
	}
	next, ok := cron.NextFireAfter(&parsed, now)
	if !ok {
		return nil
	}
	utc := next.UTC()
	return &utc
}
