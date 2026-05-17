package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/openfoundry/openfoundry-go/libs/pipeline-expression"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/domain/executor"
)

// runtimeAggregationFunc is one aggregation entry inside an aggregate
// transform. Fields mirror the catalog form fields surfaced by
// transformCatalogAggregate (group_by, aggregations[function,
// source_column, target_column]) so the authoring UI and the runtime
// share the same wire shape.
type runtimeAggregationFunc struct {
	Function     string `json:"function,omitempty"`
	SourceColumn string `json:"source_column,omitempty"`
	TargetColumn string `json:"target_column,omitempty"`
}

// supportedAggregateFunctions is the v1 set the lightweight runtime
// evaluates. Matches the operator vocabulary defined in
// libs/pipeline-plan (Phase C.1).
var supportedAggregateFunctions = map[string]struct{}{
	"sum":            {},
	"count":          {},
	"count_distinct": {},
	"avg":            {},
	"stddev":         {},
	"min":            {},
	"max":            {},
}

// runAggregate groups upstream rows by `GroupBy` and emits one row per
// group with the listed `Aggregations`. An empty GroupBy collapses
// the input to a single global row.
//
// NULL inputs are ignored by every aggregator except `count(*)` (when
// SourceColumn is empty). Numeric aggregations (sum/avg/min/max/stddev)
// require numeric inputs; non-numeric inputs surface as a typed error
// so dashboards do not silently skip them. stddev uses Welford's
// online algorithm and emits the sample standard deviation (n-1
// denominator), matching the Spark `stddev(...)` default.
func (rt *lightweightTableRuntime) runAggregate(node executor.NodeContext, cfg tableRuntimeConfig) ([]pipelineexpression.Row, error) {
	rows, err := rt.firstDependencyRows(node)
	if err != nil {
		return nil, err
	}
	if len(cfg.Aggregations) == 0 {
		return nil, errors.New("lightweight_aggregate_no_aggregations")
	}
	for i, agg := range cfg.Aggregations {
		if _, ok := supportedAggregateFunctions[agg.Function]; !ok {
			return nil, fmt.Errorf("lightweight_aggregate_unknown_function: aggregations[%d].function=%q", i, agg.Function)
		}
		if agg.Function != "count" && strings.TrimSpace(agg.SourceColumn) == "" {
			return nil, fmt.Errorf("lightweight_aggregate_missing_source_column: aggregations[%d].function=%q requires source_column", i, agg.Function)
		}
		if strings.TrimSpace(agg.TargetColumn) == "" {
			return nil, fmt.Errorf("lightweight_aggregate_missing_target_column: aggregations[%d]", i)
		}
	}

	type groupState struct {
		keys []json.RawMessage
		accs []*aggregateAccumulator
	}

	groups := map[string]*groupState{}
	order := []string{}

	for _, row := range rows {
		keyVals := make([]json.RawMessage, len(cfg.GroupBy))
		for i, k := range cfg.GroupBy {
			keyVals[i] = row[k]
		}
		key := canonicalAggregateKey(keyVals)
		gs, ok := groups[key]
		if !ok {
			gs = &groupState{
				keys: keyVals,
				accs: make([]*aggregateAccumulator, len(cfg.Aggregations)),
			}
			for i, agg := range cfg.Aggregations {
				gs.accs[i] = newAggregateAccumulator(agg.Function)
			}
			groups[key] = gs
			order = append(order, key)
		}
		for i, agg := range cfg.Aggregations {
			value := pipelineexpression.EvalNull()
			if agg.Function == "count" && agg.SourceColumn == "" {
				// count(*): every input row counts, regardless of NULL columns.
				value = pipelineexpression.EvalBool(true)
			} else if raw, present := row[agg.SourceColumn]; present {
				value = pipelineexpression.EvalValueFromJSON(raw)
			}
			if err := gs.accs[i].ingest(value); err != nil {
				return nil, fmt.Errorf("lightweight_aggregate_ingest %q: %w", agg.TargetColumn, err)
			}
		}
	}

	out := make([]pipelineexpression.Row, 0, len(order))
	for _, key := range order {
		gs := groups[key]
		row := pipelineexpression.Row{}
		for i, k := range cfg.GroupBy {
			if gs.keys[i] != nil {
				row[k] = append(json.RawMessage(nil), gs.keys[i]...)
			} else {
				row[k] = json.RawMessage("null")
			}
		}
		for i, agg := range cfg.Aggregations {
			finalised, err := gs.accs[i].finalise()
			if err != nil {
				return nil, fmt.Errorf("lightweight_aggregate_finalise %q: %w", agg.TargetColumn, err)
			}
			row[agg.TargetColumn] = finalised
		}
		out = append(out, row)
	}
	return out, nil
}

// aggregateAccumulator holds single-pass state for one aggregation
// function on one group. Allocates only the slots the function uses;
// the zero value is meaningful for every supported function.
type aggregateAccumulator struct {
	function string
	count    int64
	sum      float64
	min      float64
	max      float64
	extSet   bool
	// Welford state for stddev.
	mean float64
	m2   float64
	// count_distinct payload set (JSON-canonical key → present).
	distinct map[string]struct{}
}

func newAggregateAccumulator(function string) *aggregateAccumulator {
	a := &aggregateAccumulator{function: function}
	if function == "count_distinct" {
		a.distinct = make(map[string]struct{})
	}
	return a
}

func (a *aggregateAccumulator) ingest(v pipelineexpression.EvalValue) error {
	// SQL semantics: most aggregates ignore NULL inputs.
	if v.Kind == pipelineexpression.EvalKindNull && a.function != "count" {
		return nil
	}
	switch a.function {
	case "count":
		a.count++
	case "count_distinct":
		a.distinct[canonicalEvalKey(v)] = struct{}{}
	case "sum", "avg":
		f, ok := evalValueToFloat(v)
		if !ok {
			return fmt.Errorf("function %q requires numeric input, got kind %v", a.function, v.Kind)
		}
		a.sum += f
		a.count++
	case "min", "max":
		f, ok := evalValueToFloat(v)
		if !ok {
			return fmt.Errorf("function %q requires numeric input, got kind %v", a.function, v.Kind)
		}
		if !a.extSet {
			a.min, a.max = f, f
			a.extSet = true
			return nil
		}
		if f < a.min {
			a.min = f
		}
		if f > a.max {
			a.max = f
		}
	case "stddev":
		f, ok := evalValueToFloat(v)
		if !ok {
			return fmt.Errorf("function stddev requires numeric input, got kind %v", v.Kind)
		}
		a.count++
		delta := f - a.mean
		a.mean += delta / float64(a.count)
		delta2 := f - a.mean
		a.m2 += delta * delta2
	}
	return nil
}

func (a *aggregateAccumulator) finalise() (json.RawMessage, error) {
	switch a.function {
	case "count":
		return json.Marshal(a.count)
	case "count_distinct":
		return json.Marshal(int64(len(a.distinct)))
	case "sum":
		return json.Marshal(a.sum)
	case "avg":
		if a.count == 0 {
			return json.RawMessage("null"), nil
		}
		return json.Marshal(a.sum / float64(a.count))
	case "min":
		if !a.extSet {
			return json.RawMessage("null"), nil
		}
		return json.Marshal(a.min)
	case "max":
		if !a.extSet {
			return json.RawMessage("null"), nil
		}
		return json.Marshal(a.max)
	case "stddev":
		if a.count < 2 {
			return json.RawMessage("null"), nil
		}
		return json.Marshal(math.Sqrt(a.m2 / float64(a.count-1)))
	}
	return nil, fmt.Errorf("unsupported aggregate function: %s", a.function)
}

// evalValueToFloat converts a typed EvalValue into a float64 for
// numeric aggregation. Returns (0, false) for null and non-numeric
// kinds; the caller surfaces a typed error.
func evalValueToFloat(v pipelineexpression.EvalValue) (float64, bool) {
	switch v.Kind {
	case pipelineexpression.EvalKindInteger:
		return float64(v.Int), true
	case pipelineexpression.EvalKindDouble:
		return v.Double, true
	case pipelineexpression.EvalKindBool:
		if v.Bool {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// canonicalEvalKey returns a stable string encoding of one EvalValue
// for use as a count_distinct map key.
func canonicalEvalKey(v pipelineexpression.EvalValue) string {
	switch v.Kind {
	case pipelineexpression.EvalKindNull:
		return "\x00null"
	case pipelineexpression.EvalKindBool:
		if v.Bool {
			return "b:1"
		}
		return "b:0"
	case pipelineexpression.EvalKindInteger:
		return fmt.Sprintf("i:%d", v.Int)
	case pipelineexpression.EvalKindDouble:
		return fmt.Sprintf("d:%v", v.Double)
	case pipelineexpression.EvalKindString:
		return "s:" + v.Str
	}
	return "?"
}

// canonicalAggregateKey produces a stable map key for a tuple of
// JSON-encoded group-by values. The raw bytes are reused directly so
// equal cells produce the same key without re-marshalling.
func canonicalAggregateKey(vs []json.RawMessage) string {
	parts := make([]string, len(vs))
	for i, raw := range vs {
		if raw == nil {
			parts[i] = "\x00"
		} else {
			parts[i] = string(raw)
		}
	}
	return strings.Join(parts, "\x1f")
}
