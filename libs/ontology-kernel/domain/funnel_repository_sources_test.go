package domain

import (
	"math"
	"testing"
)

// percentileCont must match Postgres' percentile_cont aggregate:
// linear interpolation between the two ranks, clamped to [0, 1].
func TestPercentileContEmptyReturnsNil(t *testing.T) {
	t.Parallel()
	if got := percentileCont(nil, 0.95); got != nil {
		t.Fatalf("expected nil for empty input, got %v", *got)
	}
}

func TestPercentileContSingleValue(t *testing.T) {
	t.Parallel()
	got := percentileCont([]float64{42}, 0.95)
	if got == nil || *got != 42 {
		t.Fatalf("expected 42, got %v", got)
	}
}

func TestPercentileContExactMidpoint(t *testing.T) {
	t.Parallel()
	got := percentileCont([]float64{0, 10}, 0.5)
	if got == nil || math.Abs(*got-5) > 1e-9 {
		t.Fatalf("expected 5 (midpoint), got %v", got)
	}
}

func TestPercentileContP95(t *testing.T) {
	t.Parallel()
	// 100 evenly spaced points → p95 ≈ 94.05 (interp between idx 94 and 95).
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(i)
	}
	got := percentileCont(values, 0.95)
	if got == nil {
		t.Fatal("expected non-nil")
	}
	want := 94.05
	if math.Abs(*got-want) > 1e-9 {
		t.Fatalf("p95: got %v, want %v", *got, want)
	}
}

func TestPercentileContClampsPercentile(t *testing.T) {
	t.Parallel()
	values := []float64{1, 2, 3, 4, 5}
	if got := percentileCont(values, -0.5); got == nil || *got != 1 {
		t.Errorf("p<0 must clamp to first, got %v", got)
	}
	if got := percentileCont(values, 1.5); got == nil || *got != 5 {
		t.Errorf("p>1 must clamp to last, got %v", got)
	}
}
