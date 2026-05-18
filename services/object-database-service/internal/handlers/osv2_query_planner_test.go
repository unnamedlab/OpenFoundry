package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openfoundry/openfoundry-go/services/object-database-service/internal/storage"
)

func TestOSV2QueryExplainUsesHistogramAndRecordsRestrictedFilters(t *testing.T) {
	h := newTestHandlers(t)
	seedObject(t, h, "Ticket", "t1", map[string]any{"status": "open", "region": "west"})
	seedObject(t, h, "Ticket", "t2", map[string]any{"status": "closed", "region": "east"})
	seedObject(t, h, "Ticket", "t3", map[string]any{"status": "open", "region": "east"})

	mux := mountOntologyRoutes(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ontology/types/Ticket/objects/query?explain=true&restricted_view_id=rv.ticket", strings.NewReader(`{
		"filters":[{"property_name":"status","operator":"equals","value":"open"}],
		"marking_columns":["marking"]
	}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("explain got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Explain struct {
			IndexChoice   string  `json:"index_choice"`
			EstimatedRows float64 `json:"estimated_rows"`
			Steps         []struct {
				IndexName         string   `json:"index_name"`
				RestrictedFilters []string `json:"restricted_filters"`
			} `json:"steps"`
		} `json:"explain"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode explain: %v", err)
	}
	if resp.Explain.IndexChoice != "property_index_lookup" {
		t.Fatalf("expected property index plan, got %#v", resp.Explain)
	}
	if resp.Explain.EstimatedRows != 2 {
		t.Fatalf("expected histogram estimate of 2 open tickets, got %v", resp.Explain.EstimatedRows)
	}
	if len(resp.Explain.Steps) != 1 || resp.Explain.Steps[0].IndexName != "ontology_indexes.object_property_index" {
		t.Fatalf("expected property index step, got %#v", resp.Explain.Steps)
	}
	if len(resp.Explain.Steps[0].RestrictedFilters) == 0 {
		t.Fatalf("expected restricted view filters in plan")
	}
}

func TestOSV2QueryAnalyzeRecordsCostAndRewritesMaterializedAggregate(t *testing.T) {
	h := newTestHandlers(t)
	seedObject(t, h, "Order", "o1", map[string]any{"status": "open", "amount": 10})
	seedObject(t, h, "Order", "o2", map[string]any{"status": "closed", "amount": 5})
	seedObject(t, h, "Order", "o3", map[string]any{"status": "open", "amount": 7})
	store := h.Objects.(*storage.InMemoryObjectStore)
	if err := store.DeclareMaterializedAggregate(context.Background(), storage.MaterializedAggregate{Tenant: testTenant, TypeID: "Order", Function: "sum", PropertyName: "amount"}); err != nil {
		t.Fatalf("declare aggregate: %v", err)
	}

	mux := mountOntologyRoutes(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ontology/types/Order/objects/query?explain=analyze", strings.NewReader(`{
		"aggregations":[{"id":"total_amount","function":"sum","property_name":"amount"}],
		"per_page":2,
		"max_staleness_ms":5000
	}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("analyze got %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Aggregations []struct {
			Value float64 `json:"value"`
		} `json:"aggregations"`
		Actuals struct {
			RowsScanned  float64 `json:"rows_scanned"`
			RowsReturned float64 `json:"rows_returned"`
		} `json:"actuals"`
		Materialized []any `json:"materialized_aggregates"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode analyze: %v", err)
	}
	if len(resp.Aggregations) != 1 || resp.Aggregations[0].Value != 22 {
		t.Fatalf("expected materialized sum=22, got %#v", resp.Aggregations)
	}
	if resp.Actuals.RowsScanned != 3 || resp.Actuals.RowsReturned != 2 {
		t.Fatalf("unexpected actuals: %#v", resp.Actuals)
	}
	if len(resp.Materialized) != 1 {
		t.Fatalf("expected materialized aggregate rewrite metadata")
	}
	summary, err := store.QueryCostSummary(context.Background(), testTenant, "")
	if err != nil {
		t.Fatalf("cost summary: %v", err)
	}
	if summary.QueryCount != 1 || summary.RowsReturned != 2 {
		t.Fatalf("unexpected cost summary: %#v", summary)
	}
}
