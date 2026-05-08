package queryengine

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

func releaseAll(batches []arrow.RecordBatch) {
	for _, b := range batches {
		b.Release()
	}
}

func TestSelectOne(t *testing.T) {
	t.Parallel()
	qc := New()
	batches, schema, err := qc.ExecuteSQL(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	defer releaseAll(batches)

	if len(batches) != 1 {
		t.Fatalf("want 1 batch, got %d", len(batches))
	}
	rec := batches[0]
	if rec.NumRows() != 1 {
		t.Fatalf("want 1 row, got %d", rec.NumRows())
	}
	if rec.NumCols() != 1 {
		t.Fatalf("want 1 col, got %d", rec.NumCols())
	}
	col := rec.Column(0).(*array.Int64)
	if col.Value(0) != 1 {
		t.Fatalf("want value 1, got %d", col.Value(0))
	}
	if schema.Field(0).Name != "col1" {
		t.Fatalf("want col1 name, got %s", schema.Field(0).Name)
	}
}

func TestSelectMultipleLiterals(t *testing.T) {
	t.Parallel()
	qc := New()
	batches, _, err := qc.ExecuteSQL(context.Background(), "SELECT 1, 'abc', TRUE, NULL, 1.5")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	defer releaseAll(batches)
	rec := batches[0]
	if rec.NumCols() != 5 {
		t.Fatalf("want 5 cols, got %d", rec.NumCols())
	}
	if rec.Column(0).(*array.Int64).Value(0) != 1 {
		t.Fatalf("col0 mismatch")
	}
	if rec.Column(1).(*array.String).Value(0) != "abc" {
		t.Fatalf("col1 mismatch")
	}
	if !rec.Column(2).(*array.Boolean).Value(0) {
		t.Fatalf("col2 mismatch")
	}
	// Null-typed columns have no null bitmap, so IsNull() returns
	// false; check via NullN() == Len() (every value is null by
	// definition of the Null type) — matches DataFusion's
	// canonical representation for `SELECT NULL`.
	if rec.Column(3).NullN() != rec.Column(3).Len() {
		t.Fatalf("col3 must be Null-typed")
	}
	if rec.Column(4).(*array.Float64).Value(0) != 1.5 {
		t.Fatalf("col4 mismatch")
	}
}

func TestArithmeticFolding(t *testing.T) {
	t.Parallel()
	qc := New()
	cases := []struct {
		sql  string
		want int64
	}{
		{"SELECT 1 + 1", 2},
		{"SELECT 2 * 3 + 4", 10},
		{"SELECT (2 + 3) * 4", 20},
		{"SELECT 10 / 2 - 1", 4},
		{"SELECT -7", -7},
		{"SELECT 5 - -3", 8},
	}
	for _, tc := range cases {
		batches, _, err := qc.ExecuteSQL(context.Background(), tc.sql)
		if err != nil {
			t.Fatalf("%q: %v", tc.sql, err)
		}
		got := batches[0].Column(0).(*array.Int64).Value(0)
		releaseAll(batches)
		if got != tc.want {
			t.Errorf("%q = %d, want %d", tc.sql, got, tc.want)
		}
	}
}

func TestArithmeticFloatPromotion(t *testing.T) {
	t.Parallel()
	qc := New()
	batches, _, err := qc.ExecuteSQL(context.Background(), "SELECT 1.5 * 2")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	defer releaseAll(batches)
	if batches[0].Column(0).(*array.Float64).Value(0) != 3.0 {
		t.Fatalf("float promotion mismatch")
	}
}

func TestUnsupportedFromIsRejected(t *testing.T) {
	t.Parallel()
	qc := New()
	_, _, err := qc.ExecuteSQL(context.Background(), "SELECT * FROM iceberg.t1")
	if !errors.Is(err, ErrUnsupportedLocalExecution) {
		t.Fatalf("want ErrUnsupportedLocalExecution, got %v", err)
	}
}

func TestUnsupportedJoinIsRejected(t *testing.T) {
	t.Parallel()
	qc := New()
	_, _, err := qc.ExecuteSQL(context.Background(), "SELECT 1 FROM t JOIN s ON t.id=s.id")
	if !errors.Is(err, ErrUnsupportedLocalExecution) {
		t.Fatalf("want ErrUnsupportedLocalExecution, got %v", err)
	}
}

func TestEmptyStatementIsRejected(t *testing.T) {
	t.Parallel()
	qc := New()
	_, _, err := qc.ExecuteSQL(context.Background(), "")
	if !errors.Is(err, ErrUnsupportedLocalExecution) {
		t.Fatalf("want ErrUnsupportedLocalExecution, got %v", err)
	}
}

func TestNullPropagation(t *testing.T) {
	t.Parallel()
	qc := New()
	batches, _, err := qc.ExecuteSQL(context.Background(), "SELECT NULL + 1")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	defer releaseAll(batches)
	col := batches[0].Column(0)
	if col.NullN() != col.Len() {
		t.Fatalf("NULL + 1 must propagate to NULL (Null-typed column)")
	}
}

func TestStringEscapeIsParsed(t *testing.T) {
	t.Parallel()
	qc := New()
	batches, _, err := qc.ExecuteSQL(context.Background(), "SELECT 'it''s'")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	defer releaseAll(batches)
	if batches[0].Column(0).(*array.String).Value(0) != "it's" {
		t.Fatalf("escape not unfolded")
	}
}

func TestTrailingSemicolonIsAllowed(t *testing.T) {
	t.Parallel()
	qc := New()
	batches, _, err := qc.ExecuteSQL(context.Background(), "SELECT 1;")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	releaseAll(batches)
}
