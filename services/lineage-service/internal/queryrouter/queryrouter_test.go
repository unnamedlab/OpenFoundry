package queryrouter

import "testing"

func TestRouteLastHourCassandra(t *testing.T) {
	t.Parallel()
	if got := Route(1_000_000, 1_000_000-3600); got != SourceCassandra {
		t.Fatalf("got %v", got)
	}
}

func TestRouteOverOneDayTrino(t *testing.T) {
	t.Parallel()
	if got := Route(1_000_000, 1_000_000-(25*3600)); got != SourceTrino {
		t.Fatalf("got %v", got)
	}
}

func TestRouteBoundaryAt24hHot(t *testing.T) {
	t.Parallel()
	// exactly 24h → still Cassandra (inclusive).
	if got := Route(1_000_000, 1_000_000-(24*3600)); got != SourceCassandra {
		t.Fatalf("got %v", got)
	}
}

func TestRouteWindow(t *testing.T) {
	t.Parallel()
	if got := RouteWindow(24); got != SourceCassandra {
		t.Fatalf("24h got %v", got)
	}
	if got := RouteWindow(25); got != SourceTrino {
		t.Fatalf("25h got %v", got)
	}
}

func TestMetricLabelsStable(t *testing.T) {
	t.Parallel()
	if SourceCassandra.AsMetricLabel() != "cassandra" {
		t.Fatal("cassandra label")
	}
	if SourceTrino.AsMetricLabel() != "trino" {
		t.Fatal("trino label")
	}
}

func TestTrinoDisabledByDefault(t *testing.T) {
	t.Parallel()
	if TrinoEnabledFromEnv(nil) {
		t.Fatal("nil should be disabled")
	}
	empty := ""
	if TrinoEnabledFromEnv(&empty) {
		t.Fatal("empty should be disabled")
	}
	zero := "0"
	if TrinoEnabledFromEnv(&zero) {
		t.Fatal("0 should be disabled")
	}
	one := "true"
	if !TrinoEnabledFromEnv(&one) {
		t.Fatal("true should be enabled")
	}
	num := "1"
	if !TrinoEnabledFromEnv(&num) {
		t.Fatal("1 should be enabled")
	}
}

func TestFullGraphDefaultsToHistoricalAndDegradesWithoutTrino(t *testing.T) {
	t.Parallel()
	p := Plan(KindFullGraph, nil, false, false)
	if p.WindowHours != 25 {
		t.Fatalf("window got %d", p.WindowHours)
	}
	if p.RequestedSource != SourceTrino {
		t.Fatalf("requested got %v", p.RequestedSource)
	}
	if p.SelectedSource != SourceCassandra {
		t.Fatalf("selected got %v", p.SelectedSource)
	}
	if !p.Degraded {
		t.Fatal("expected degraded")
	}
}

func TestDatasetQueriesDefaultToHotPath(t *testing.T) {
	t.Parallel()
	p := Plan(KindDatasetGraph, nil, false, false)
	if p.WindowHours != 24 {
		t.Fatalf("window got %d", p.WindowHours)
	}
	if p.RequestedSource != SourceCassandra || p.SelectedSource != SourceCassandra {
		t.Fatalf("got %+v", p)
	}
	if p.Degraded {
		t.Fatal("hot path should not degrade")
	}
}

func TestHistoricalFlagForcesHistoricalWindow(t *testing.T) {
	t.Parallel()
	one := uint32(1)
	p := Plan(KindDatasetImpact, &one, true, false)
	if p.WindowHours != 25 {
		t.Fatalf("window got %d", p.WindowHours)
	}
	if p.RequestedSource != SourceTrino {
		t.Fatalf("requested got %v", p.RequestedSource)
	}
	if !p.Degraded {
		t.Fatal("expected degraded")
	}
}

func TestTrinoRequiresFlagAndImplementation(t *testing.T) {
	t.Parallel()
	v := "true"
	if TrinoAvailableFromEnv(&v) {
		t.Fatal("flag alone should not suffice while reader unimplemented")
	}
	if TrinoAvailableFromEnv(nil) {
		t.Fatal("nil should not be available")
	}
}
