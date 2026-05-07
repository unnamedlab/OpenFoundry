package funnel

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

func sampleSource(status string) models.OntologyFunnelSource {
	return models.OntologyFunnelSource{
		ID:               uuid.New(),
		Name:             "tickets-batch",
		ObjectTypeID:     uuid.New(),
		DatasetID:        uuid.New(),
		PreviewLimit:     100,
		DefaultMarking:   "public",
		Status:           status,
		PropertyMappings: []models.OntologyFunnelPropertyMapping{},
		OwnerID:          uuid.New(),
	}
}

func sampleMetrics(latest *string, totalRuns int64, lastRunAt *time.Time) models.OntologyFunnelHealthMetricsRow {
	out := models.OntologyFunnelHealthMetricsRow{
		TotalRuns:       totalRuns,
		LatestRunStatus: latest,
		LastRunAt:       lastRunAt,
		RowsRead:        100,
		InsertedCount:   40,
		UpdatedCount:    60,
	}
	if latest != nil {
		switch *latest {
		case "completed", "dry_run":
			out.SuccessfulRuns = totalRuns
			out.LastSuccessAt = lastRunAt
		case "failed":
			out.FailedRuns = 1
			out.LastFailureAt = lastRunAt
		case "completed_with_errors", "dry_run_with_errors":
			out.WarningRuns = 1
			out.LastWarningAt = lastRunAt
			out.ErrorCount = 3
		}
	}
	return out
}

// Mirrors `classifies_healthy_source_when_latest_run_completed`.
func TestBuildSourceHealth_HealthyOnLatestCompleted(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	completed := "completed"
	got := BuildSourceHealth(sampleSource("active"), sampleMetrics(&completed, 4, &now), 24)
	if got.HealthStatus != "healthy" {
		t.Fatalf("expected healthy, got %s", got.HealthStatus)
	}
}

// Mirrors `classifies_failing_source_when_latest_run_failed`.
func TestBuildSourceHealth_FailingOnLatestFailed(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	failed := "failed"
	got := BuildSourceHealth(sampleSource("active"), sampleMetrics(&failed, 4, &now), 24)
	if got.HealthStatus != "failing" {
		t.Fatalf("expected failing, got %s", got.HealthStatus)
	}
}

// Mirrors `classifies_stale_source_when_last_run_is_too_old`.
func TestBuildSourceHealth_StaleWhenLastRunTooOld(t *testing.T) {
	t.Parallel()
	old := time.Now().UTC().Add(-48 * time.Hour)
	completed := "completed"
	got := BuildSourceHealth(sampleSource("active"), sampleMetrics(&completed, 4, &old), 24)
	if got.HealthStatus != "stale" {
		t.Fatalf("expected stale, got %s (last_run_at=%v)", got.HealthStatus, got.LastRunAt)
	}
}

// Mirrors `classifies_paused_source_before_considering_runs`.
func TestBuildSourceHealth_PausedBeforeRunsConsidered(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	failed := "failed"
	got := BuildSourceHealth(sampleSource("paused"), sampleMetrics(&failed, 4, &now), 24)
	if got.HealthStatus != "paused" {
		t.Fatalf("expected paused, got %s", got.HealthStatus)
	}
}

// Mirrors `classifies_never_run_source_without_history`.
func TestBuildSourceHealth_NeverRunWithoutHistory(t *testing.T) {
	t.Parallel()
	got := BuildSourceHealth(sampleSource("active"), sampleMetrics(nil, 0, nil), 24)
	if got.HealthStatus != "never_run" {
		t.Fatalf("expected never_run, got %s", got.HealthStatus)
	}
}

func TestFunnelHealthSortRank(t *testing.T) {
	t.Parallel()
	cases := map[string]int{
		"failing":   0,
		"degraded":  1,
		"stale":     2,
		"never_run": 3,
		"paused":    4,
		"healthy":   5,
		"unknown":   6,
	}
	for status, want := range cases {
		if got := funnelHealthSortRank(status); got != want {
			t.Errorf("rank(%s) = %d, want %d", status, got, want)
		}
	}
}

func TestEnsureOwnerOrAdmin(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	other := uuid.New()
	// admin bypass.
	if err := ensureOwnerOrAdmin(owner, mustClaims(other, []string{"admin"})); err != nil {
		t.Fatalf("admin must bypass: %v", err)
	}
	// owner OK.
	if err := ensureOwnerOrAdmin(owner, mustClaims(owner, []string{"member"})); err != nil {
		t.Fatalf("owner must pass: %v", err)
	}
	// non-owner non-admin → forbidden.
	if err := ensureOwnerOrAdmin(owner, mustClaims(other, []string{"member"})); err == nil {
		t.Fatal("expected forbidden")
	}
}

func TestValidateSourceStatus(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"active", "paused", "  active  "} {
		if err := validateSourceStatus(ok); err != nil {
			t.Errorf("expected %q to be valid: %v", ok, err)
		}
	}
	if err := validateSourceStatus("draft"); err == nil {
		t.Fatal("expected draft to fail")
	}
}

func TestClampPreviewLimit(t *testing.T) {
	t.Parallel()
	cases := map[int32]int32{0: 1, 500: 500, 5000: 1000}
	for in, want := range cases {
		if got := clampPreviewLimit(in); got != want {
			t.Errorf("clamp(%d) = %d, want %d", in, got, want)
		}
	}
}
