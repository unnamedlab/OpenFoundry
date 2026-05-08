package queryengine

import (
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// ErrUnsupportedLocalExecution is returned by [QueryContext.ExecuteSQL]
// when the SQL statement is not handled by the literal evaluator.
// Callers (the Flight SQL gateway) translate this to a clear
// FailedPrecondition status with the warehousing-endpoint hint —
// matching how DataFusion-not-installed maps to a configuration
// error rather than an execution one.
var ErrUnsupportedLocalExecution = errors.New(
	"queryengine: local execution requires a literal SELECT; " +
		"federate the statement by setting WAREHOUSING_FLIGHT_SQL_URL")

// QueryContext is the local SQL execution context handed to the
// Flight SQL service. Mirrors `query_engine::QueryContext`.
type QueryContext struct {
	alloc memory.Allocator
}

// New builds a QueryContext using the default Arrow allocator.
// Mirrors `QueryContext::new()`.
func New() *QueryContext { return &QueryContext{alloc: memory.DefaultAllocator} }

// NewWithAllocator builds a QueryContext using a caller-provided
// allocator. Useful for tests that want strict bookkeeping.
func NewWithAllocator(alloc memory.Allocator) *QueryContext {
	return &QueryContext{alloc: alloc}
}

// Allocator returns the allocator the context will hand to Arrow
// builders. Useful for callers that want to compose batches before
// streaming them through the same allocator.
func (c *QueryContext) Allocator() memory.Allocator { return c.alloc }

// SQL runs `query` and returns the first record batch (or nil) plus
// the result schema. Mirrors `QueryContext::sql` on the Rust side
// modulo DataFusion's lazy DataFrame: the Go literal evaluator is
// strict, so the returned record (if any) is fully materialised.
//
// SQL is a thin wrapper over [QueryContext.ExecuteSQL] kept for
// surface parity with the Rust API; consumers that need the full
// batch slice should call ExecuteSQL directly.
func (c *QueryContext) SQL(ctx context.Context, query string) (arrow.RecordBatch, *arrow.Schema, error) {
	batches, schema, err := c.ExecuteSQL(ctx, query)
	if err != nil {
		return nil, nil, err
	}
	if len(batches) == 0 {
		return nil, schema, nil
	}
	first := batches[0]
	for _, b := range batches[1:] {
		b.Release()
	}
	return first, schema, nil
}

// ExplainSQL returns the (logical, physical) plans for `query`.
// Mirrors `QueryContext::explain_sql` on the Rust side. The Go
// literal evaluator has no planner, so the returned plans are
// trivial textual summaries of the parsed expression list. Callers
// targeting the production EXPLAIN path should federate the
// statement to a backend that ships a real planner.
func (c *QueryContext) ExplainSQL(ctx context.Context, query string) (string, string, error) {
	exprs, err := parseLiteralSelect(query)
	if err != nil {
		return "", "", err
	}
	logical := fmt.Sprintf("LiteralProjection: %d expr(s)", len(exprs))
	physical := fmt.Sprintf("LiteralProjectionExec: %d expr(s) — fold-to-single-row", len(exprs))
	return logical, physical, nil
}

// RegisterParquet registers a Parquet file as a table. Mirrors
// `QueryContext::register_parquet`. The Go runtime ships no
// catalog/planner, so this returns [ErrUnsupportedLocalExecution];
// callers should federate the statement against a backend with
// `WAREHOUSING_FLIGHT_SQL_URL` set.
func (c *QueryContext) RegisterParquet(_ context.Context, _ string, _ string) error {
	return ErrUnsupportedLocalExecution
}

// RegisterCSV registers a CSV file as a table. Mirrors
// `QueryContext::register_csv`. See [QueryContext.RegisterParquet]
// for why this returns [ErrUnsupportedLocalExecution].
func (c *QueryContext) RegisterCSV(_ context.Context, _ string, _ string) error {
	return ErrUnsupportedLocalExecution
}

// RegisterBatch registers an in-memory RecordBatch as a table.
// Mirrors `QueryContext::register_batch`. See
// [QueryContext.RegisterParquet] for why this returns
// [ErrUnsupportedLocalExecution].
func (c *QueryContext) RegisterBatch(_ string, _ arrow.RecordBatch) error {
	return ErrUnsupportedLocalExecution
}

// ExecuteSQL runs sql against the local literal evaluator.
//
// On success it returns a slice with exactly one
// [arrow.RecordBatch] (mirroring how DataFusion's literal-only
// queries fold to a single batch in the Rust impl) plus the schema
// of that batch. Callers must call Release on every returned record
// when they're done with them.
//
// On unsupported SQL the function returns
// [ErrUnsupportedLocalExecution] so the caller can decide whether
// to translate it to a clear configuration error.
func (c *QueryContext) ExecuteSQL(_ context.Context, sql string) ([]arrow.RecordBatch, *arrow.Schema, error) {
	exprs, err := parseLiteralSelect(sql)
	if err != nil {
		return nil, nil, err
	}
	values := make([]literalValue, len(exprs))
	for i, e := range exprs {
		v, err := evalExpr(e)
		if err != nil {
			return nil, nil, err
		}
		values[i] = v
	}
	return buildSingleRowBatch(c.alloc, values)
}

// buildSingleRowBatch constructs a one-row record batch with one
// column per literal value. Each column is named `colN` to mirror
// DataFusion's anonymous-column naming for un-aliased SELECT exprs.
func buildSingleRowBatch(alloc memory.Allocator, values []literalValue) ([]arrow.RecordBatch, *arrow.Schema, error) {
	fields := make([]arrow.Field, len(values))
	cols := make([]arrow.Array, len(values))
	for i, v := range values {
		f, col, err := buildColumn(alloc, fmt.Sprintf("col%d", i+1), v)
		if err != nil {
			// Release any columns built so far before bailing out.
			for j := 0; j < i; j++ {
				cols[j].Release()
			}
			return nil, nil, err
		}
		fields[i] = f
		cols[i] = col
	}
	schema := arrow.NewSchema(fields, nil)
	rec := array.NewRecordBatch(schema, cols, 1)
	// NewRecordBatch retains the columns; release our local handles.
	for _, c := range cols {
		c.Release()
	}
	return []arrow.RecordBatch{rec}, schema, nil
}

func buildColumn(alloc memory.Allocator, name string, v literalValue) (arrow.Field, arrow.Array, error) {
	switch v.kind {
	case kindInt:
		b := array.NewInt64Builder(alloc)
		defer b.Release()
		b.Append(v.intVal)
		return arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Int64, Nullable: false}, b.NewArray(), nil
	case kindFloat:
		b := array.NewFloat64Builder(alloc)
		defer b.Release()
		b.Append(v.floatVal)
		return arrow.Field{Name: name, Type: arrow.PrimitiveTypes.Float64, Nullable: false}, b.NewArray(), nil
	case kindBool:
		b := array.NewBooleanBuilder(alloc)
		defer b.Release()
		b.Append(v.boolVal)
		return arrow.Field{Name: name, Type: arrow.FixedWidthTypes.Boolean, Nullable: false}, b.NewArray(), nil
	case kindString:
		b := array.NewStringBuilder(alloc)
		defer b.Release()
		b.Append(v.stringVal)
		return arrow.Field{Name: name, Type: arrow.BinaryTypes.String, Nullable: false}, b.NewArray(), nil
	case kindNull:
		// Use NullType for the canonical "untyped null" column —
		// Arrow's null-array length 1 with null count 1.
		b := array.NewNullBuilder(alloc)
		defer b.Release()
		b.AppendNull()
		return arrow.Field{Name: name, Type: arrow.Null, Nullable: true}, b.NewArray(), nil
	default:
		return arrow.Field{}, nil, fmt.Errorf("queryengine: unknown literal kind %d", v.kind)
	}
}
