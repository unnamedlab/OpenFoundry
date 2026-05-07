package routing

import (
	"testing"

	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/config"
)

func cfg(warehousing, vespa, postgres string) *config.Config {
	return cfgWithTrino(warehousing, vespa, postgres, "")
}

func cfgWithTrino(warehousing, vespa, postgres, trino string) *config.Config {
	c := &config.Config{}
	c.Host = "127.0.0.1"
	c.DatabaseURL = "postgres://test"
	c.JWTSecret = "test"
	c.WarehousingFlightSQLURL = warehousing
	c.VespaFlightSQLURL = vespa
	c.PostgresFlightSQLURL = postgres
	c.TrinoFlightSQLURL = trino
	return c
}

// Mirrors the Rust unit `classifies_by_first_catalog_prefix`.
func TestClassifiesByFirstCatalogPrefix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		sql  string
		want Backend
	}{
		{"SELECT 1", BackendIceberg},
		{"SELECT * FROM iceberg.sales.orders", BackendIceberg},
		{"SELECT * FROM vespa.documents", BackendVespa},
		{"SELECT * FROM postgres.public.users", BackendPostgres},
		{"SELECT * FROM postgresql.public.users", BackendPostgres},
		{"SELECT * FROM trino.of_lineage.runs", BackendTrino},
	}
	for _, tc := range cases {
		if got := Classify(tc.sql); got != tc.want {
			t.Errorf("classify(%q) = %s, want %s", tc.sql, got, tc.want)
		}
	}
}

// Mirrors `trino_routes_to_configured_endpoint`.
func TestTrinoRoutesToConfiguredEndpoint(t *testing.T) {
	t.Parallel()
	r := FromConfig(cfgWithTrino("", "", "", "http://trino-flight-sql-proxy.trino:50133"))
	d, err := r.Route("SELECT * FROM trino.of_metrics_long.service_metrics_daily")
	if err != nil {
		t.Fatalf("trino route should succeed when configured: %v", err)
	}
	if d.Backend != BackendTrino {
		t.Fatalf("want trino, got %s", d.Backend)
	}
	if d.RemoteURL != "http://trino-flight-sql-proxy.trino:50133" {
		t.Fatalf("unexpected endpoint: %s", d.RemoteURL)
	}
}

// Mirrors `missing_trino_endpoint_is_an_explicit_error`.
func TestMissingTrinoEndpointIsExplicitError(t *testing.T) {
	t.Parallel()
	r := FromConfig(cfg("", "", ""))
	_, err := r.Route("SELECT * FROM trino.of_lineage.runs")
	if !IsBackendUnavailable(err) {
		t.Fatalf("expected ErrBackendUnavailable, got %v", err)
	}
}

// Mirrors `backend_all_includes_trino`.
func TestAllBackendsIncludesTrino(t *testing.T) {
	t.Parallel()
	all := AllBackends()
	found := false
	for _, b := range all {
		if b == BackendTrino {
			found = true
		}
	}
	if !found {
		t.Fatalf("AllBackends missing trino: %v", all)
	}
	if string(BackendTrino) != "trino" {
		t.Fatalf("trino as_str mismatch: %s", BackendTrino)
	}
}

// Mirrors `join_and_update_are_recognised_as_table_anchors`.
func TestJoinAndUpdateAreTableAnchors(t *testing.T) {
	t.Parallel()
	if got := Classify("SELECT * FROM iceberg.t1 JOIN vespa.t2 USING(id)"); got != BackendIceberg {
		t.Fatalf("first FROM target wins: got %s", got)
	}
	if got := Classify("UPDATE postgres.public.users SET active=true"); got != BackendPostgres {
		t.Fatalf("UPDATE anchor: got %s", got)
	}
}

// Mirrors `local_iceberg_routes_to_local_datafusion_when_warehousing_unconfigured`.
func TestLocalIcebergWhenWarehousingUnconfigured(t *testing.T) {
	t.Parallel()
	r := FromConfig(cfg("", "", ""))
	d, err := r.Route("SELECT 1")
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if d.Backend != BackendIceberg {
		t.Fatalf("want iceberg, got %s", d.Backend)
	}
	if d.RemoteURL != "" {
		t.Fatalf("expected local execution (empty url), got %s", d.RemoteURL)
	}
}

// Mirrors `iceberg_delegates_to_warehousing_when_configured`.
func TestIcebergDelegatesToWarehousingWhenConfigured(t *testing.T) {
	t.Parallel()
	r := FromConfig(cfg("http://sql-warehousing-service:50123", "", ""))
	d, err := r.Route("SELECT * FROM iceberg.sales.orders")
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if d.Backend != BackendIceberg {
		t.Fatalf("want iceberg, got %s", d.Backend)
	}
	if d.RemoteURL != "http://sql-warehousing-service:50123" {
		t.Fatalf("unexpected endpoint: %s", d.RemoteURL)
	}
}

// Mirrors `empty_endpoint_string_is_treated_as_unconfigured`.
func TestEmptyEndpointTreatedAsUnconfigured(t *testing.T) {
	t.Parallel()
	r := FromConfig(cfg("   ", "", ""))
	d, err := r.Route("SELECT 1")
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if d.RemoteURL != "" {
		t.Fatalf("expected empty url after normalize, got %s", d.RemoteURL)
	}
}
