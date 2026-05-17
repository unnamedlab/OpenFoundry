package pipelineplan

import pipelineexpression "github.com/openfoundry/openfoundry-go/libs/pipeline-expression"

// ReadTable pulls every row from an Iceberg table at the current
// snapshot. Source operator — has no inputs.
type ReadTable struct {
	Catalog   string `json:"catalog"`
	Namespace string `json:"namespace"`
	Table     string `json:"table"`
}

// FullyQualified renders "catalog.namespace.table" — the form the
// Spark runner accepted as `--input-dataset` / `--output-dataset`.
func (r ReadTable) FullyQualified() string {
	return r.Catalog + "." + r.Namespace + "." + r.Table
}

// Filter keeps only rows for which Expr evaluates to true. Expr is a
// pipeline-expression DSL string that must type-check to BOOLEAN under
// the upstream schema. Empty Expr is invalid.
type Filter struct {
	Expr string `json:"expr"`
}

// Project rewrites the row to exactly the listed columns. Each column
// either selects an existing column by name (Expr left empty) or
// produces a derived value from a pipeline-expression DSL string.
// Order in Columns is the output column order.
type Project struct {
	Columns []ProjectColumn `json:"columns"`
}

// ProjectColumn is one output column of a [Project] op.
type ProjectColumn struct {
	Name string `json:"name"`
	Expr string `json:"expr,omitempty"`
}

// Passthrough reports whether this column is a no-op selection of an
// existing upstream column with the same name.
func (c ProjectColumn) Passthrough() bool { return c.Expr == "" }

// Rename relabels upstream columns. Each mapping renames From → To;
// columns not listed pass through unchanged.
type Rename struct {
	Mapping []ColumnPair `json:"mapping"`
}

// ColumnPair is one rename mapping.
type ColumnPair struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Cast converts the listed columns to a different type. Other columns
// pass through unchanged. The interpreter is responsible for honouring
// the pipeline-expression promotion rules; unrepresentable cells
// surface as a runtime error, not as silent NULLs.
type Cast struct {
	Casts []ColumnCast `json:"casts"`
}

// ColumnCast is one column cast.
type ColumnCast struct {
	Column string                          `json:"column"`
	To     pipelineexpression.PipelineType `json:"to"`
}

// Aggregate groups by zero or more columns and produces one row per
// group with the listed aggregations. An empty GroupBy collapses the
// input to a single global row (the SQL `SELECT … FROM t` shape).
type Aggregate struct {
	GroupBy      []string          `json:"group_by"`
	Aggregations []AggregationFunc `json:"aggregations"`
}

// AggregationFunc is one aggregation entry inside an [Aggregate] op.
//
// Function values map to the canonical SQL aggregates the Phase 0
// inventory enumerated as required: sum, count, count_distinct, avg,
// stddev, min, max. SourceColumn is empty only for `count` (SQL
// `count(*)`); every other function requires it.
type AggregationFunc struct {
	Function     string `json:"function"`
	SourceColumn string `json:"source_column,omitempty"`
	TargetColumn string `json:"target_column"`
}

// AllAggregations is the pinned set of aggregation function tokens.
// Adding to this list is wire-compatible; removing is breaking.
func AllAggregations() []string {
	return []string{"sum", "count", "count_distinct", "avg", "stddev", "min", "max"}
}

// IsAggregation reports whether name is a recognised aggregation
// function.
func IsAggregation(name string) bool {
	for _, v := range AllAggregations() {
		if v == name {
			return true
		}
	}
	return false
}

// Union concatenates two or more upstream row streams. Schemas must
// be compatible by column name and type; a mismatch is a validation
// error. Order across upstreams is unspecified.
type Union struct{}

// Limit caps the number of rows. N must be > 0; 0 is rejected (an
// explicit "drop everything" is not a valid pipeline step).
type Limit struct {
	N int64 `json:"n"`
}

// WriteTable commits the upstream row stream as a new Iceberg snapshot
// on the target table. Terminal operator — has no downstream.
type WriteTable struct {
	Catalog   string    `json:"catalog"`
	Namespace string    `json:"namespace"`
	Table     string    `json:"table"`
	Mode      WriteMode `json:"mode"`
}

// FullyQualified renders "catalog.namespace.table".
func (w WriteTable) FullyQualified() string {
	return w.Catalog + "." + w.Namespace + "." + w.Table
}

// WriteMode selects the Iceberg snapshot semantics for [WriteTable].
type WriteMode string

const (
	// WriteModeCreateOrReplace publishes a new snapshot that fully
	// replaces the table's contents — matches the Spark sink's
	// `df.writeTo(target).createOrReplace()` semantics.
	WriteModeCreateOrReplace WriteMode = "create_or_replace"
	// WriteModeAppend publishes a new snapshot that adds rows to the
	// existing table without rewriting prior snapshots.
	WriteModeAppend WriteMode = "append"
)

// IsWriteMode reports whether m is a recognised write mode.
func IsWriteMode(m WriteMode) bool {
	switch m {
	case WriteModeCreateOrReplace, WriteModeAppend:
		return true
	}
	return false
}
