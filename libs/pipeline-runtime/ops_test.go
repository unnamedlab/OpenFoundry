package pipelineruntime

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	pipelineexpression "github.com/openfoundry/openfoundry-go/libs/pipeline-expression"
	pp "github.com/openfoundry/openfoundry-go/libs/pipeline-plan"
)

// rowsToStream wraps an in-memory slice as a RowStream.
func rowsToStream(rows []Row) RowStream {
	return func(yield func(Row, error) bool) {
		for _, r := range rows {
			if !yield(r, nil) {
				return
			}
		}
	}
}

func collect(t *testing.T, s RowStream) []Row {
	t.Helper()
	var out []Row
	for r, err := range s {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func TestBuildFilter_parseErrorSurfaces(t *testing.T) {
	t.Parallel()
	_, err := buildFilter(rowsToStream(nil), pp.Filter{Expr: "(((((bad"})
	if err == nil || !strings.Contains(err.Error(), "parse filter expression") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestBuildFilter_runtimeTypeError(t *testing.T) {
	t.Parallel()
	// Expr evaluates to a string, not bool — filter should reject.
	s, err := buildFilter(rowsToStream([]Row{{"x": "hello"}}), pp.Filter{Expr: "x"})
	if err != nil {
		t.Fatalf("buildFilter: %v", err)
	}
	var seenErr error
	for _, e := range s {
		if e != nil {
			seenErr = e
		}
	}
	if seenErr == nil || !strings.Contains(seenErr.Error(), "expected BOOLEAN") {
		t.Errorf("expected BOOLEAN type error, got %v", seenErr)
	}
}

func TestBuildProject_passthroughAndExpr(t *testing.T) {
	t.Parallel()
	s, err := buildProject(rowsToStream([]Row{
		{"a": int64(2), "b": int64(3), "extra": "drop"},
	}), pp.Project{Columns: []pp.ProjectColumn{
		{Name: "a"},                                     // passthrough
		{Name: "doubled", Expr: "a * 2"},
		{Name: "sum_ab", Expr: "a + b"},
	}})
	if err != nil {
		t.Fatalf("buildProject: %v", err)
	}
	out := collect(t, s)
	if len(out) != 1 {
		t.Fatalf("rows = %d", len(out))
	}
	r := out[0]
	if r["a"].(int64) != 2 {
		t.Errorf("passthrough a = %v", r["a"])
	}
	if r["doubled"].(int64) != 4 {
		t.Errorf("doubled = %v", r["doubled"])
	}
	if r["sum_ab"].(int64) != 5 {
		t.Errorf("sum_ab = %v", r["sum_ab"])
	}
	if _, present := r["extra"]; present {
		t.Error("`extra` should be dropped by project")
	}
}

func TestBuildRename_renamesAndPreserves(t *testing.T) {
	t.Parallel()
	s := buildRename(rowsToStream([]Row{{"a": int64(1), "b": int64(2)}}),
		pp.Rename{Mapping: []pp.ColumnPair{{From: "a", To: "alpha"}}})
	r := collect(t, s)[0]
	if r["alpha"] != int64(1) {
		t.Errorf("renamed alpha = %v", r["alpha"])
	}
	if r["b"] != int64(2) {
		t.Errorf("unrelated b = %v", r["b"])
	}
	if _, present := r["a"]; present {
		t.Error("`a` should be removed after rename")
	}
}

func TestBuildCast_basicConversions(t *testing.T) {
	t.Parallel()
	s := buildCast(rowsToStream([]Row{
		{"a": "42", "b": int64(3), "c": "1.5", "d": "true"},
	}), pp.Cast{Casts: []pp.ColumnCast{
		{Column: "a", To: pipelineexpression.LongType()},
		{Column: "b", To: pipelineexpression.DoubleType()},
		{Column: "c", To: pipelineexpression.DoubleType()},
		{Column: "d", To: pipelineexpression.BooleanType()},
	}})
	r := collect(t, s)[0]
	if r["a"].(int64) != 42 {
		t.Errorf("a cast = %v", r["a"])
	}
	if r["b"].(float64) != 3 {
		t.Errorf("b cast = %v", r["b"])
	}
	if r["c"].(float64) != 1.5 {
		t.Errorf("c cast = %v", r["c"])
	}
	if r["d"].(bool) != true {
		t.Errorf("d cast = %v", r["d"])
	}
}

func TestBuildCast_unrepresentableErrors(t *testing.T) {
	t.Parallel()
	s := buildCast(rowsToStream([]Row{{"x": "not-a-number"}}),
		pp.Cast{Casts: []pp.ColumnCast{{Column: "x", To: pipelineexpression.LongType()}}})
	var seen error
	for _, err := range s {
		if err != nil {
			seen = err
		}
	}
	if seen == nil || !strings.Contains(seen.Error(), "INTEGER/LONG") {
		t.Errorf("expected cast error, got %v", seen)
	}
}

func TestBuildAggregate_sumAndCountAndCountDistinct(t *testing.T) {
	t.Parallel()
	s := buildAggregate(rowsToStream([]Row{
		{"g": "A", "v": float64(1), "k": "x"},
		{"g": "A", "v": float64(2), "k": "x"},
		{"g": "A", "v": float64(3), "k": "y"},
		{"g": "B", "v": float64(10), "k": "x"},
	}), pp.Aggregate{
		GroupBy: []string{"g"},
		Aggregations: []pp.AggregationFunc{
			{Function: "sum", SourceColumn: "v", TargetColumn: "s"},
			{Function: "count", TargetColumn: "n"},
			{Function: "count_distinct", SourceColumn: "k", TargetColumn: "kd"},
		},
	})
	out := collect(t, s)
	if len(out) != 2 {
		t.Fatalf("groups = %d", len(out))
	}
	byG := map[string]Row{}
	for _, r := range out {
		byG[r["g"].(string)] = r
	}
	if byG["A"]["s"].(float64) != 6 {
		t.Errorf("A.sum = %v", byG["A"]["s"])
	}
	if byG["A"]["n"].(int64) != 3 {
		t.Errorf("A.count = %v", byG["A"]["n"])
	}
	if byG["A"]["kd"].(int64) != 2 {
		t.Errorf("A.count_distinct = %v", byG["A"]["kd"])
	}
	if byG["B"]["s"].(float64) != 10 {
		t.Errorf("B.sum = %v", byG["B"]["s"])
	}
	if byG["B"]["n"].(int64) != 1 {
		t.Errorf("B.count = %v", byG["B"]["n"])
	}
}

func TestBuildAggregate_avgMinMaxStddev(t *testing.T) {
	t.Parallel()
	s := buildAggregate(rowsToStream([]Row{
		{"v": float64(2)}, {"v": float64(4)}, {"v": float64(4)}, {"v": float64(4)},
		{"v": float64(5)}, {"v": float64(5)}, {"v": float64(7)}, {"v": float64(9)},
	}), pp.Aggregate{
		GroupBy: nil, // single global group
		Aggregations: []pp.AggregationFunc{
			{Function: "avg", SourceColumn: "v", TargetColumn: "a"},
			{Function: "min", SourceColumn: "v", TargetColumn: "mn"},
			{Function: "max", SourceColumn: "v", TargetColumn: "mx"},
			{Function: "stddev", SourceColumn: "v", TargetColumn: "sd"},
		},
	})
	out := collect(t, s)
	if len(out) != 1 {
		t.Fatalf("expected single global row, got %d", len(out))
	}
	r := out[0]
	if r["a"].(float64) != 5 {
		t.Errorf("avg = %v, want 5", r["a"])
	}
	if r["mn"].(float64) != 2 {
		t.Errorf("min = %v", r["mn"])
	}
	if r["mx"].(float64) != 9 {
		t.Errorf("max = %v", r["mx"])
	}
	// Well-known stats sample stddev for [2,4,4,4,5,5,7,9] = 2.138...
	if got := r["sd"].(float64); math.Abs(got-2.138) > 0.01 {
		t.Errorf("stddev = %v, want ~2.138", got)
	}
}

func TestBuildAggregate_ignoresNulls(t *testing.T) {
	t.Parallel()
	s := buildAggregate(rowsToStream([]Row{
		{"v": float64(10)},
		{"v": nil},
		{"v": float64(20)},
	}), pp.Aggregate{Aggregations: []pp.AggregationFunc{
		{Function: "sum", SourceColumn: "v", TargetColumn: "s"},
		{Function: "count", SourceColumn: "v", TargetColumn: "n"},
	}})
	r := collect(t, s)[0]
	if r["s"].(float64) != 30 {
		t.Errorf("sum should skip null: got %v", r["s"])
	}
	// count without source_column is count(*); here we supplied a
	// SourceColumn so count counts non-null v values too.
	if r["n"].(int64) != 3 {
		t.Errorf("count incremented per call regardless of null: got %v", r["n"])
	}
}

func TestBuildUnion_concatenatesInOrder(t *testing.T) {
	t.Parallel()
	a := rowsToStream([]Row{{"x": int64(1)}, {"x": int64(2)}})
	b := rowsToStream([]Row{{"x": int64(3)}})
	s := buildUnion([]RowStream{a, b})
	out := collect(t, s)
	xs := make([]int64, 0, len(out))
	for _, r := range out {
		xs = append(xs, r["x"].(int64))
	}
	if !slices.Equal(xs, []int64{1, 2, 3}) {
		t.Errorf("union order = %v, want [1 2 3]", xs)
	}
}

func TestBuildLimit_stopsAtN(t *testing.T) {
	t.Parallel()
	in := rowsToStream([]Row{{"x": int64(1)}, {"x": int64(2)}, {"x": int64(3)}, {"x": int64(4)}})
	s := buildLimit(in, pp.Limit{N: 2})
	out := collect(t, s)
	if len(out) != 2 {
		t.Errorf("limited = %d, want 2", len(out))
	}
}

func TestBuildOp_unhandledKindReturnsError(t *testing.T) {
	t.Parallel()
	op := pp.Op{ID: "x", Kind: pp.Kind("write_table")} // terminal — should never reach buildOp
	_, err := buildOp(context.Background(), op, nil)
	if err == nil || !strings.Contains(err.Error(), "unhandled kind") {
		t.Errorf("expected unhandled-kind error, got %v", err)
	}
}

func TestErrStream_yieldsOnce(t *testing.T) {
	t.Parallel()
	myErr := errors.New("boom")
	s := errStream(myErr)
	called := 0
	for _, err := range s {
		called++
		if !errors.Is(err, myErr) {
			t.Errorf("expected %v, got %v", myErr, err)
		}
	}
	if called != 1 {
		t.Errorf("errStream should yield once, called %d", called)
	}
}
