package pipelineruntime

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	pipelineexpression "github.com/openfoundry/openfoundry-go/libs/pipeline-expression"
	pp "github.com/openfoundry/openfoundry-go/libs/pipeline-plan"
)

// buildOp produces the RowStream for a non-source, non-terminal op
// given its upstreams. Returns a setup error (parse failures, etc.)
// — per-row errors are surfaced through the returned stream.
//
// Source ops (read_table) and terminal ops (write_table) are handled
// directly by [Executor.Run] and never reach here.
func buildOp(_ context.Context, op pp.Op, upstreams []RowStream) (RowStream, error) {
	switch op.Kind {
	case pp.KindFilter:
		return buildFilter(upstreams[0], *op.Filter)
	case pp.KindProject:
		return buildProject(upstreams[0], *op.Project)
	case pp.KindRename:
		return buildRename(upstreams[0], *op.Rename), nil
	case pp.KindCast:
		return buildCast(upstreams[0], *op.Cast), nil
	case pp.KindAggregate:
		return buildAggregate(upstreams[0], *op.Aggregate), nil
	case pp.KindUnion:
		return buildUnion(upstreams), nil
	case pp.KindLimit:
		return buildLimit(upstreams[0], *op.Limit), nil
	default:
		return nil, fmt.Errorf("buildOp: unhandled kind %q", op.Kind)
	}
}

// ---- filter ----

func buildFilter(upstream RowStream, f pp.Filter) (RowStream, error) {
	parsed, err := pipelineexpression.ParseExpr(f.Expr)
	if err != nil {
		return nil, fmt.Errorf("parse filter expression %q: %w", f.Expr, err)
	}
	return func(yield func(Row, error) bool) {
		for row, err := range upstream {
			if err != nil {
				yield(nil, err)
				return
			}
			ok, err := evalExprToBool(parsed, row)
			if err != nil {
				yield(nil, err)
				return
			}
			if !ok {
				continue
			}
			if !yield(row, nil) {
				return
			}
		}
	}, nil
}

// ---- project ----

type projectColumn struct {
	name        string
	passthrough bool
	parsed      pipelineexpression.Expr
}

func buildProject(upstream RowStream, p pp.Project) (RowStream, error) {
	cols := make([]projectColumn, len(p.Columns))
	for i, c := range p.Columns {
		if c.Passthrough() {
			cols[i] = projectColumn{name: c.Name, passthrough: true}
			continue
		}
		parsed, err := pipelineexpression.ParseExpr(c.Expr)
		if err != nil {
			return nil, fmt.Errorf("parse project column %q expression %q: %w", c.Name, c.Expr, err)
		}
		cols[i] = projectColumn{name: c.Name, parsed: parsed}
	}
	return func(yield func(Row, error) bool) {
		for row, err := range upstream {
			if err != nil {
				yield(nil, err)
				return
			}
			out := make(Row, len(cols))
			for _, c := range cols {
				if c.passthrough {
					out[c.name] = row[c.name]
					continue
				}
				v, err := evalExprToAny(c.parsed, row)
				if err != nil {
					yield(nil, fmt.Errorf("evaluate project column %q: %w", c.name, err))
					return
				}
				out[c.name] = v
			}
			if !yield(out, nil) {
				return
			}
		}
	}, nil
}

// ---- rename ----

func buildRename(upstream RowStream, r pp.Rename) RowStream {
	mapping := make(map[string]string, len(r.Mapping))
	for _, m := range r.Mapping {
		mapping[m.From] = m.To
	}
	return func(yield func(Row, error) bool) {
		for row, err := range upstream {
			if err != nil {
				yield(nil, err)
				return
			}
			out := make(Row, len(row))
			for k, v := range row {
				if to, ok := mapping[k]; ok {
					out[to] = v
				} else {
					out[k] = v
				}
			}
			if !yield(out, nil) {
				return
			}
		}
	}
}

// ---- cast ----

func buildCast(upstream RowStream, c pp.Cast) RowStream {
	return func(yield func(Row, error) bool) {
		for row, err := range upstream {
			if err != nil {
				yield(nil, err)
				return
			}
			out := make(Row, len(row))
			for k, v := range row {
				out[k] = v
			}
			for _, cc := range c.Casts {
				converted, err := castValue(out[cc.Column], cc.To)
				if err != nil {
					yield(nil, fmt.Errorf("cast column %q: %w", cc.Column, err))
					return
				}
				out[cc.Column] = converted
			}
			if !yield(out, nil) {
				return
			}
		}
	}
}

// ---- aggregate ----

// aggAccumulator holds online state for one aggregation function on
// one group. The struct carries every possible piece of state; the
// `function` field tells which subset is meaningful.
//
// stddev uses Welford's online algorithm for numerical stability.
type aggAccumulator struct {
	function string
	count    int64
	sum      float64
	min      float64
	max      float64
	minSet   bool
	// Welford state.
	mean float64
	m2   float64
	// for count_distinct.
	distinct map[string]struct{}
}

func newAccumulator(function string) *aggAccumulator {
	a := &aggAccumulator{function: function}
	if function == "count_distinct" {
		a.distinct = make(map[string]struct{})
	}
	return a
}

func (a *aggAccumulator) ingest(value any) error {
	if value == nil && a.function != "count" {
		// SQL: most aggregates ignore NULL inputs.
		return nil
	}
	switch a.function {
	case "count":
		a.count++
	case "count_distinct":
		key := canonicalKey(value)
		a.distinct[key] = struct{}{}
	case "min", "max":
		f, err := castToFloat64(value)
		if err != nil {
			// Non-numeric min/max via string compare — defer to canonicalKey.
			s := canonicalKey(value)
			if !a.minSet {
				a.min = 0
				a.max = 0
				a.minSet = true
				a.distinct = map[string]struct{}{s: {}}
				// We piggyback the distinct map to hold the running string extremum.
				return nil
			}
			// String comparison branch
			for cur := range a.distinct {
				if (a.function == "min" && s < cur) || (a.function == "max" && s > cur) {
					delete(a.distinct, cur)
					a.distinct[s] = struct{}{}
				}
				break
			}
			return nil
		}
		if !a.minSet {
			a.min, a.max = f, f
			a.minSet = true
		}
		if f < a.min {
			a.min = f
		}
		if f > a.max {
			a.max = f
		}
	case "sum", "avg":
		f, err := castToFloat64(value)
		if err != nil {
			return fmt.Errorf("aggregate %q requires numeric input: %w", a.function, err)
		}
		a.sum += f
		a.count++
	case "stddev":
		f, err := castToFloat64(value)
		if err != nil {
			return fmt.Errorf("aggregate stddev requires numeric input: %w", err)
		}
		a.count++
		delta := f - a.mean
		a.mean += delta / float64(a.count)
		delta2 := f - a.mean
		a.m2 += delta * delta2
	default:
		return fmt.Errorf("aggregate function %q is not implemented", a.function)
	}
	return nil
}

// finalize returns the runtime value the aggregator's group emits.
func (a *aggAccumulator) finalize() any {
	switch a.function {
	case "count":
		return a.count
	case "count_distinct":
		return int64(len(a.distinct))
	case "sum":
		return a.sum
	case "avg":
		if a.count == 0 {
			return nil
		}
		return a.sum / float64(a.count)
	case "min":
		if !a.minSet {
			return nil
		}
		if a.distinct != nil {
			// String-extremum path.
			for s := range a.distinct {
				return s
			}
		}
		return a.min
	case "max":
		if !a.minSet {
			return nil
		}
		if a.distinct != nil {
			for s := range a.distinct {
				return s
			}
		}
		return a.max
	case "stddev":
		if a.count < 2 {
			return nil
		}
		// Sample standard deviation (n-1 in the denominator) — matches
		// Spark's stddev(...) default.
		return math.Sqrt(a.m2 / float64(a.count-1))
	}
	return nil
}

// groupState bundles per-group accumulators in stable agg order.
type groupState struct {
	keys []any
	accs []*aggAccumulator
}

func buildAggregate(upstream RowStream, a pp.Aggregate) RowStream {
	return func(yield func(Row, error) bool) {
		groups := map[string]*groupState{}
		// stable iteration order for emit: insertion order
		order := []string{}

		for row, err := range upstream {
			if err != nil {
				yield(nil, err)
				return
			}
			keyVals := make([]any, len(a.GroupBy))
			for i, k := range a.GroupBy {
				keyVals[i] = row[k]
			}
			key := canonicalKeyN(keyVals)
			gs, ok := groups[key]
			if !ok {
				gs = &groupState{
					keys: keyVals,
					accs: make([]*aggAccumulator, len(a.Aggregations)),
				}
				for i, agg := range a.Aggregations {
					gs.accs[i] = newAccumulator(agg.Function)
				}
				groups[key] = gs
				order = append(order, key)
			}
			for i, agg := range a.Aggregations {
				var val any
				if agg.Function == "count" && agg.SourceColumn == "" {
					val = struct{}{} // sentinel — count(*) increments regardless of NULLs
				} else {
					val = row[agg.SourceColumn]
				}
				if err := gs.accs[i].ingest(val); err != nil {
					yield(nil, fmt.Errorf("aggregate %q: %w", agg.TargetColumn, err))
					return
				}
			}
		}

		for _, key := range order {
			gs := groups[key]
			out := make(Row, len(a.GroupBy)+len(a.Aggregations))
			for i, k := range a.GroupBy {
				out[k] = gs.keys[i]
			}
			for i, agg := range a.Aggregations {
				out[agg.TargetColumn] = gs.accs[i].finalize()
			}
			if !yield(out, nil) {
				return
			}
		}
	}
}

// canonicalKey returns a stable string encoding of a single value for
// use as a map key (in count_distinct and the group encoding).
func canonicalKey(v any) string {
	if v == nil {
		return "\x00null"
	}
	switch x := v.(type) {
	case string:
		return "s:" + x
	case bool:
		if x {
			return "b:1"
		}
		return "b:0"
	default:
		return fmt.Sprintf("v:%v", v)
	}
}

func canonicalKeyN(vs []any) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = canonicalKey(v)
	}
	return strings.Join(parts, "\x1f")
}

// ---- union ----

func buildUnion(upstreams []RowStream) RowStream {
	return func(yield func(Row, error) bool) {
		for _, u := range upstreams {
			for row, err := range u {
				if err != nil {
					yield(nil, err)
					return
				}
				if !yield(row, nil) {
					return
				}
			}
		}
	}
}

// ---- limit ----

func buildLimit(upstream RowStream, l pp.Limit) RowStream {
	return func(yield func(Row, error) bool) {
		var n int64
		for row, err := range upstream {
			if err != nil {
				yield(nil, err)
				return
			}
			if n >= l.N {
				return
			}
			n++
			if !yield(row, nil) {
				return
			}
		}
	}
}

// Compile-time exhaustiveness check: every v1 Kind except read_table
// and write_table is handled by buildOp. If a new Kind ships in
// pipeline-plan and is not handled here, buildOp returns the
// "unhandled kind" error which the test suite catches.
var _ = sort.Strings // keep `sort` imported when build tags trim usage above
