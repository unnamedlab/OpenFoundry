package pipelineplan_test

import (
	"encoding/json"
	"strings"
	"testing"

	pipelineexpression "github.com/openfoundry/openfoundry-go/libs/pipeline-expression"
	pp "github.com/openfoundry/openfoundry-go/libs/pipeline-plan"
)

// transactionsClean is the simplest realistic Phase 0 pipeline:
// SELECT projection + WHERE filter + write back as a new Iceberg
// snapshot. Single source, single terminal, three intermediate ops.
func transactionsClean() pp.Plan {
	return pp.Plan{
		PipelineID: "online-retail-clean",
		RunID:      "run-001",
		Ops: []pp.Op{
			{
				ID: "src", Kind: pp.KindReadTable,
				ReadTable: &pp.ReadTable{Catalog: "lakekeeper", Namespace: "default", Table: "online_retail_raw"},
			},
			{
				ID: "f1", Kind: pp.KindFilter, Inputs: []string{"src"},
				Filter: &pp.Filter{Expr: "quantity > 0 AND price > 0 AND customer_id IS NOT NULL"},
			},
			{
				ID: "p1", Kind: pp.KindProject, Inputs: []string{"f1"},
				Project: &pp.Project{Columns: []pp.ProjectColumn{
					{Name: "transaction_id", Expr: "concat(invoice, '_', stockcode)"},
					{Name: "invoice"}, {Name: "stockcode"}, {Name: "description"},
					{Name: "quantity"}, {Name: "invoice_date"}, {Name: "price"},
					{Name: "customer_id"}, {Name: "country"},
					{Name: "revenue", Expr: "cast(quantity * price as double)"},
				}},
			},
			{
				ID: "sink", Kind: pp.KindWriteTable, Inputs: []string{"p1"},
				WriteTable: &pp.WriteTable{
					Catalog: "lakekeeper", Namespace: "default", Table: "transactions_clean",
					Mode: pp.WriteModeCreateOrReplace,
				},
			},
		},
	}
}

// customerMetrics is the bucket-(b) aggregate pipeline from Phase 0:
// GROUP BY customer_id with sum / count_distinct aggregations.
func customerMetrics() pp.Plan {
	return pp.Plan{
		PipelineID: "online-retail-cust",
		RunID:      "run-002",
		Ops: []pp.Op{
			{
				ID: "src", Kind: pp.KindReadTable,
				ReadTable: &pp.ReadTable{Catalog: "lakekeeper", Namespace: "default", Table: "transactions_clean"},
			},
			{
				ID: "agg", Kind: pp.KindAggregate, Inputs: []string{"src"},
				Aggregate: &pp.Aggregate{
					GroupBy: []string{"customer_id"},
					Aggregations: []pp.AggregationFunc{
						{Function: "sum", SourceColumn: "revenue", TargetColumn: "total_revenue"},
						{Function: "count_distinct", SourceColumn: "invoice", TargetColumn: "num_orders"},
						{Function: "count_distinct", SourceColumn: "country", TargetColumn: "num_countries"},
					},
				},
			},
			{
				ID: "sink", Kind: pp.KindWriteTable, Inputs: []string{"agg"},
				WriteTable: &pp.WriteTable{
					Catalog: "lakekeeper", Namespace: "default", Table: "customer_metrics",
					Mode: pp.WriteModeCreateOrReplace,
				},
			},
		},
	}
}

// limitedPreview exercises rename + cast + limit (operators not used
// in the two PoC plans above) so the test surface covers every v1 op.
func limitedPreview() pp.Plan {
	return pp.Plan{
		PipelineID: "preview",
		RunID:      "run-003",
		Ops: []pp.Op{
			{
				ID: "src", Kind: pp.KindReadTable,
				ReadTable: &pp.ReadTable{Catalog: "lakekeeper", Namespace: "default", Table: "online_retail_raw"},
			},
			{
				ID: "r1", Kind: pp.KindRename, Inputs: []string{"src"},
				Rename: &pp.Rename{Mapping: []pp.ColumnPair{
					{From: "invoice", To: "invoice_number"},
				}},
			},
			{
				ID: "c1", Kind: pp.KindCast, Inputs: []string{"r1"},
				Cast: &pp.Cast{Casts: []pp.ColumnCast{
					{Column: "quantity", To: pipelineexpression.LongType()},
				}},
			},
			{
				ID: "l1", Kind: pp.KindLimit, Inputs: []string{"c1"},
				Limit: &pp.Limit{N: 100},
			},
			{
				ID: "sink", Kind: pp.KindWriteTable, Inputs: []string{"l1"},
				WriteTable: &pp.WriteTable{
					Catalog: "lakekeeper", Namespace: "default", Table: "preview_sample",
					Mode: pp.WriteModeAppend,
				},
			},
		},
	}
}

// unionTwoSources covers the only multi-input v1 op so the validation
// branch for `union requires ≥2 inputs` is exercised by a valid case.
func unionTwoSources() pp.Plan {
	return pp.Plan{
		PipelineID: "u",
		RunID:      "run-004",
		Ops: []pp.Op{
			{
				ID: "a", Kind: pp.KindReadTable,
				ReadTable: &pp.ReadTable{Catalog: "lakekeeper", Namespace: "default", Table: "t_a"},
			},
			{
				ID: "b", Kind: pp.KindReadTable,
				ReadTable: &pp.ReadTable{Catalog: "lakekeeper", Namespace: "default", Table: "t_b"},
			},
			{
				ID: "u", Kind: pp.KindUnion, Inputs: []string{"a", "b"},
				Union: &pp.Union{},
			},
			{
				ID: "sink", Kind: pp.KindWriteTable, Inputs: []string{"u"},
				WriteTable: &pp.WriteTable{
					Catalog: "lakekeeper", Namespace: "default", Table: "t_ab",
					Mode: pp.WriteModeAppend,
				},
			},
		},
	}
}

func TestValidate_acceptsKnownGoodPlans(t *testing.T) {
	t.Parallel()
	cases := map[string]pp.Plan{
		"transactionsClean": transactionsClean(),
		"customerMetrics":   customerMetrics(),
		"limitedPreview":    limitedPreview(),
		"unionTwoSources":   unionTwoSources(),
	}
	for name, plan := range cases {
		plan := plan
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if errs := plan.Validate(); errs != nil {
				t.Fatalf("expected valid plan, got errors: %v", errs)
			}
		})
	}
}

func TestJSON_roundTrip(t *testing.T) {
	t.Parallel()
	plans := map[string]pp.Plan{
		"transactionsClean": transactionsClean(),
		"customerMetrics":   customerMetrics(),
		"limitedPreview":    limitedPreview(),
		"unionTwoSources":   unionTwoSources(),
	}
	for name, plan := range plans {
		plan := plan
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var back pp.Plan
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if errs := back.Validate(); errs != nil {
				t.Errorf("unmarshalled plan failed validate: %v", errs)
			}
			again, _ := json.MarshalIndent(back, "", "  ")
			if string(raw) != string(again) {
				t.Errorf("round-trip not stable\nfirst: %s\nthen:  %s", raw, again)
			}
		})
	}
}

func TestJSON_unmarshalUnknownKind(t *testing.T) {
	t.Parallel()
	raw := `{"pipeline_id":"x","run_id":"y","ops":[
		{"id":"a","kind":"weld"}
	]}`
	var plan pp.Plan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	errs := plan.Validate()
	if errs == nil {
		t.Fatal("expected validation errors")
	}
	if !strings.Contains(errs.Error(), `unknown kind "weld"`) {
		t.Errorf("error should mention unknown kind, got: %v", errs)
	}
}

func TestAllKinds_isStableAndExhaustive(t *testing.T) {
	t.Parallel()
	got := pp.AllKinds()
	want := []pp.Kind{
		pp.KindReadTable, pp.KindFilter, pp.KindProject, pp.KindRename, pp.KindCast,
		pp.KindAggregate, pp.KindUnion, pp.KindLimit, pp.KindWriteTable,
	}
	if len(got) != len(want) {
		t.Fatalf("AllKinds count = %d, want %d", len(got), len(want))
	}
	for i, k := range want {
		if got[i] != k {
			t.Errorf("AllKinds[%d] = %q, want %q", i, got[i], k)
		}
	}
	for _, k := range got {
		if !pp.IsKind(k) {
			t.Errorf("IsKind(%q) false but in AllKinds", k)
		}
	}
}

func TestSourceAndTerminal(t *testing.T) {
	t.Parallel()
	plan := transactionsClean()
	var sources, terminals int
	for _, op := range plan.Ops {
		if op.Source() {
			sources++
		}
		if op.Terminal() {
			terminals++
		}
	}
	if sources != 1 {
		t.Errorf("source count = %d, want 1", sources)
	}
	if terminals != 1 {
		t.Errorf("terminal count = %d, want 1", terminals)
	}
}

func TestReadTable_FullyQualified(t *testing.T) {
	t.Parallel()
	r := pp.ReadTable{Catalog: "lakekeeper", Namespace: "default", Table: "t"}
	if got := r.FullyQualified(); got != "lakekeeper.default.t" {
		t.Errorf("got %q", got)
	}
}

func TestProjectColumn_Passthrough(t *testing.T) {
	t.Parallel()
	if !(pp.ProjectColumn{Name: "x"}).Passthrough() {
		t.Error("empty Expr should be passthrough")
	}
	if (pp.ProjectColumn{Name: "x", Expr: "1+1"}).Passthrough() {
		t.Error("non-empty Expr should not be passthrough")
	}
}
