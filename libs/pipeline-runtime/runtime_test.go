package pipelineruntime_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	pp "github.com/openfoundry/openfoundry-go/libs/pipeline-plan"
	pr "github.com/openfoundry/openfoundry-go/libs/pipeline-runtime"
)

// ---- fakes ----

// memReader serves pre-loaded rows per "catalog.namespace.table" key.
type memReader struct {
	mu    sync.Mutex
	rows  map[string][]pr.Row
	calls []string
	err   error
}

func newMemReader() *memReader { return &memReader{rows: map[string][]pr.Row{}} }

func (r *memReader) put(catalog, ns, table string, rows []pr.Row) {
	r.rows[catalog+"."+ns+"."+table] = rows
}

func (r *memReader) Scan(_ context.Context, catalog, ns, table string) (pr.RowStream, error) {
	r.mu.Lock()
	r.calls = append(r.calls, catalog+"."+ns+"."+table)
	r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	rows := r.rows[catalog+"."+ns+"."+table]
	cp := make([]pr.Row, len(rows))
	copy(cp, rows)
	return func(yield func(pr.Row, error) bool) {
		for _, row := range cp {
			if !yield(row, nil) {
				return
			}
		}
	}, nil
}

// memWriter records every Write call so tests can assert against it.
type memWriter struct {
	mu     sync.Mutex
	writes []writeCall
	err    error
}

type writeCall struct {
	Catalog, Namespace, Table string
	Mode                      pp.WriteMode
	Rows                      []pr.Row
}

func (w *memWriter) Write(_ context.Context, catalog, ns, table string, mode pp.WriteMode, rows []pr.Row) error {
	if w.err != nil {
		return w.err
	}
	cp := make([]pr.Row, len(rows))
	copy(cp, rows)
	w.mu.Lock()
	w.writes = append(w.writes, writeCall{catalog, ns, table, mode, cp})
	w.mu.Unlock()
	return nil
}

// ---- plan helpers (mirrors of the C.1 test fixtures, kept local so
//      this package can build without depending on the test fixtures
//      of pipeline-plan) ----

func transactionsCleanPlan() pp.Plan {
	return pp.Plan{
		PipelineID: "online-retail-clean",
		RunID:      "r",
		Ops: []pp.Op{
			{ID: "src", Kind: pp.KindReadTable,
				ReadTable: &pp.ReadTable{Catalog: "lakekeeper", Namespace: "default", Table: "online_retail_raw"}},
			{ID: "f1", Kind: pp.KindFilter, Inputs: []string{"src"},
				Filter: &pp.Filter{Expr: "quantity > 0 AND price > 0"}},
			{ID: "p1", Kind: pp.KindProject, Inputs: []string{"f1"},
				Project: &pp.Project{Columns: []pp.ProjectColumn{
					{Name: "transaction_id", Expr: "invoice"},
					{Name: "quantity"},
					{Name: "price"},
					{Name: "revenue", Expr: "quantity * price"},
				}}},
			{ID: "sink", Kind: pp.KindWriteTable, Inputs: []string{"p1"},
				WriteTable: &pp.WriteTable{Catalog: "lakekeeper", Namespace: "default", Table: "transactions_clean", Mode: pp.WriteModeCreateOrReplace}},
		},
	}
}

func customerMetricsPlan() pp.Plan {
	return pp.Plan{
		PipelineID: "online-retail-cust",
		RunID:      "r",
		Ops: []pp.Op{
			{ID: "src", Kind: pp.KindReadTable,
				ReadTable: &pp.ReadTable{Catalog: "lakekeeper", Namespace: "default", Table: "transactions_clean"}},
			{ID: "agg", Kind: pp.KindAggregate, Inputs: []string{"src"},
				Aggregate: &pp.Aggregate{
					GroupBy: []string{"customer_id"},
					Aggregations: []pp.AggregationFunc{
						{Function: "sum", SourceColumn: "revenue", TargetColumn: "total_revenue"},
						{Function: "count_distinct", SourceColumn: "invoice", TargetColumn: "num_orders"},
					},
				}},
			{ID: "sink", Kind: pp.KindWriteTable, Inputs: []string{"agg"},
				WriteTable: &pp.WriteTable{Catalog: "lakekeeper", Namespace: "default", Table: "customer_metrics", Mode: pp.WriteModeCreateOrReplace}},
		},
	}
}

// ---- end-to-end tests ----

func TestExecutor_transactionsClean(t *testing.T) {
	t.Parallel()
	reader := newMemReader()
	reader.put("lakekeeper", "default", "online_retail_raw", []pr.Row{
		{"invoice": "INV1", "quantity": int64(3), "price": float64(2.5)},
		{"invoice": "INV2", "quantity": int64(0), "price": float64(5)}, // filtered out
		{"invoice": "INV3", "quantity": int64(-1), "price": float64(7)}, // filtered out
		{"invoice": "INV4", "quantity": int64(4), "price": float64(1.25)},
	})
	writer := &memWriter{}
	exec := &pr.Executor{Reader: reader, Writer: writer}

	if err := exec.Run(context.Background(), transactionsCleanPlan()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(writer.writes) != 1 {
		t.Fatalf("expected 1 write call, got %d", len(writer.writes))
	}
	w := writer.writes[0]
	if w.Catalog != "lakekeeper" || w.Namespace != "default" || w.Table != "transactions_clean" {
		t.Errorf("write target = %s.%s.%s", w.Catalog, w.Namespace, w.Table)
	}
	if w.Mode != pp.WriteModeCreateOrReplace {
		t.Errorf("mode = %q", w.Mode)
	}
	if len(w.Rows) != 2 {
		t.Fatalf("rows written = %d, want 2 (after filter)", len(w.Rows))
	}
	got := map[string]float64{}
	for _, r := range w.Rows {
		id := r["transaction_id"].(string)
		got[id] = mustFloat(t, r["revenue"])
	}
	want := map[string]float64{"INV1": 7.5, "INV4": 5}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("revenue[%s] = %v, want %v", k, got[k], v)
		}
	}
}

func TestExecutor_customerMetrics(t *testing.T) {
	t.Parallel()
	reader := newMemReader()
	reader.put("lakekeeper", "default", "transactions_clean", []pr.Row{
		{"customer_id": "C1", "invoice": "INV1", "revenue": float64(10)},
		{"customer_id": "C1", "invoice": "INV1", "revenue": float64(2)},
		{"customer_id": "C1", "invoice": "INV2", "revenue": float64(5)},
		{"customer_id": "C2", "invoice": "INV3", "revenue": float64(7)},
	})
	writer := &memWriter{}
	exec := &pr.Executor{Reader: reader, Writer: writer}

	if err := exec.Run(context.Background(), customerMetricsPlan()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(writer.writes[0].Rows) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(writer.writes[0].Rows))
	}
	byCust := map[string]pr.Row{}
	for _, r := range writer.writes[0].Rows {
		byCust[r["customer_id"].(string)] = r
	}
	if byCust["C1"]["total_revenue"].(float64) != 17 {
		t.Errorf("C1 total_revenue = %v, want 17", byCust["C1"]["total_revenue"])
	}
	if byCust["C1"]["num_orders"].(int64) != 2 {
		t.Errorf("C1 num_orders (distinct invoices) = %v, want 2", byCust["C1"]["num_orders"])
	}
	if byCust["C2"]["total_revenue"].(float64) != 7 {
		t.Errorf("C2 total_revenue = %v, want 7", byCust["C2"]["total_revenue"])
	}
	if byCust["C2"]["num_orders"].(int64) != 1 {
		t.Errorf("C2 num_orders = %v, want 1", byCust["C2"]["num_orders"])
	}
}

func TestExecutor_validatesPlanBeforeRunning(t *testing.T) {
	t.Parallel()
	exec := &pr.Executor{Reader: newMemReader(), Writer: &memWriter{}}
	// empty plan
	err := exec.Run(context.Background(), pp.Plan{})
	if err == nil || !strings.Contains(err.Error(), "plan invalid") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestExecutor_requiresReaderAndWriter(t *testing.T) {
	t.Parallel()
	err := (&pr.Executor{Writer: &memWriter{}}).Run(context.Background(), transactionsCleanPlan())
	if err == nil || !strings.Contains(err.Error(), "Reader is nil") {
		t.Errorf("expected reader required error, got %v", err)
	}
	err = (&pr.Executor{Reader: newMemReader()}).Run(context.Background(), transactionsCleanPlan())
	if err == nil || !strings.Contains(err.Error(), "Writer is nil") {
		t.Errorf("expected writer required error, got %v", err)
	}
}

func TestExecutor_propagatesReaderError(t *testing.T) {
	t.Parallel()
	r := newMemReader()
	r.err = errors.New("catalog down")
	exec := &pr.Executor{Reader: r, Writer: &memWriter{}}
	err := exec.Run(context.Background(), transactionsCleanPlan())
	if err == nil || !strings.Contains(err.Error(), "catalog down") {
		t.Errorf("expected wrapped reader error, got %v", err)
	}
}

func TestExecutor_propagatesWriterError(t *testing.T) {
	t.Parallel()
	r := newMemReader()
	r.put("lakekeeper", "default", "online_retail_raw", []pr.Row{
		{"invoice": "x", "quantity": int64(1), "price": float64(1)},
	})
	w := &memWriter{err: errors.New("adapter exploded")}
	exec := &pr.Executor{Reader: r, Writer: w}
	err := exec.Run(context.Background(), transactionsCleanPlan())
	if err == nil || !strings.Contains(err.Error(), "adapter exploded") {
		t.Errorf("expected wrapped writer error, got %v", err)
	}
}

func TestExecutor_unionTwoSources(t *testing.T) {
	t.Parallel()
	r := newMemReader()
	r.put("lakekeeper", "default", "t_a", []pr.Row{{"x": int64(1)}, {"x": int64(2)}})
	r.put("lakekeeper", "default", "t_b", []pr.Row{{"x": int64(3)}})
	w := &memWriter{}
	plan := pp.Plan{
		Ops: []pp.Op{
			{ID: "a", Kind: pp.KindReadTable,
				ReadTable: &pp.ReadTable{Catalog: "lakekeeper", Namespace: "default", Table: "t_a"}},
			{ID: "b", Kind: pp.KindReadTable,
				ReadTable: &pp.ReadTable{Catalog: "lakekeeper", Namespace: "default", Table: "t_b"}},
			{ID: "u", Kind: pp.KindUnion, Inputs: []string{"a", "b"}, Union: &pp.Union{}},
			{ID: "w", Kind: pp.KindWriteTable, Inputs: []string{"u"},
				WriteTable: &pp.WriteTable{Catalog: "lakekeeper", Namespace: "default", Table: "t_ab", Mode: pp.WriteModeAppend}},
		},
	}
	if err := (&pr.Executor{Reader: r, Writer: w}).Run(context.Background(), plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(w.writes[0].Rows); got != 3 {
		t.Fatalf("expected 3 rows after union, got %d", got)
	}
}

func TestExecutor_limit(t *testing.T) {
	t.Parallel()
	r := newMemReader()
	r.put("lakekeeper", "default", "src", []pr.Row{
		{"x": int64(1)}, {"x": int64(2)}, {"x": int64(3)}, {"x": int64(4)},
	})
	w := &memWriter{}
	plan := pp.Plan{
		Ops: []pp.Op{
			{ID: "s", Kind: pp.KindReadTable,
				ReadTable: &pp.ReadTable{Catalog: "lakekeeper", Namespace: "default", Table: "src"}},
			{ID: "l", Kind: pp.KindLimit, Inputs: []string{"s"}, Limit: &pp.Limit{N: 2}},
			{ID: "w", Kind: pp.KindWriteTable, Inputs: []string{"l"},
				WriteTable: &pp.WriteTable{Catalog: "lakekeeper", Namespace: "default", Table: "dst", Mode: pp.WriteModeAppend}},
		},
	}
	if err := (&pr.Executor{Reader: r, Writer: w}).Run(context.Background(), plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(w.writes[0].Rows); got != 2 {
		t.Errorf("expected 2 rows after limit, got %d", got)
	}
}

func TestExecutor_topoSortHandlesArbitraryOrder(t *testing.T) {
	t.Parallel()
	r := newMemReader()
	r.put("c", "n", "t", []pr.Row{{"x": int64(1)}, {"x": int64(2)}})
	w := &memWriter{}
	// Ops listed in reverse declaration order on purpose.
	plan := pp.Plan{
		Ops: []pp.Op{
			{ID: "w", Kind: pp.KindWriteTable, Inputs: []string{"l"},
				WriteTable: &pp.WriteTable{Catalog: "c", Namespace: "n", Table: "t2", Mode: pp.WriteModeAppend}},
			{ID: "l", Kind: pp.KindLimit, Inputs: []string{"s"}, Limit: &pp.Limit{N: 10}},
			{ID: "s", Kind: pp.KindReadTable, ReadTable: &pp.ReadTable{Catalog: "c", Namespace: "n", Table: "t"}},
		},
	}
	if err := (&pr.Executor{Reader: r, Writer: w}).Run(context.Background(), plan); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(w.writes[0].Rows); got != 2 {
		t.Errorf("got %d rows, want 2", got)
	}
}

// ---- helpers ----

func mustFloat(t *testing.T, v any) float64 {
	t.Helper()
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int64:
		return float64(x)
	case int:
		return float64(x)
	default:
		t.Fatalf("expected number, got %T (%v)", v, v)
		return 0
	}
}
