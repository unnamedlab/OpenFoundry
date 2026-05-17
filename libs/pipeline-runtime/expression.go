package pipelineruntime

import (
	"encoding/json"
	"fmt"
	"strconv"

	pipelineexpression "github.com/openfoundry/openfoundry-go/libs/pipeline-expression"
)

// toExprRow converts the runtime Row (column → arbitrary Go value)
// into the JSON-RawMessage shape pipeline-expression's evaluator
// consumes. A marshal error per-cell becomes a null value — the
// evaluator already treats malformed cells as null and SQL semantics
// match that.
func toExprRow(row Row) (pipelineexpression.Row, error) {
	out := make(pipelineexpression.Row, len(row))
	for k, v := range row {
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("encode column %q: %w", k, err)
		}
		out[k] = raw
	}
	return out, nil
}

// evalValueToAny turns the typed evaluator output back into a native
// Go value so downstream ops can keep operating on Row directly.
func evalValueToAny(v pipelineexpression.EvalValue) any {
	switch v.Kind {
	case pipelineexpression.EvalKindBool:
		return v.Bool
	case pipelineexpression.EvalKindInteger:
		return v.Int
	case pipelineexpression.EvalKindDouble:
		return v.Double
	case pipelineexpression.EvalKindString:
		return v.Str
	case pipelineexpression.EvalKindNull:
		return nil
	}
	return nil
}

// evalExprToBool parses+evaluates an expression to a boolean. NULL
// is treated as false, matching SQL WHERE semantics. Non-boolean
// scalar kinds are an error — the validation layer cannot catch
// type-mistyped expressions today, so the evaluator does at runtime.
func evalExprToBool(parsed pipelineexpression.Expr, row Row) (bool, error) {
	exprRow, err := toExprRow(row)
	if err != nil {
		return false, err
	}
	val, err := pipelineexpression.Eval(parsed, exprRow)
	if err != nil {
		return false, err
	}
	switch val.Kind {
	case pipelineexpression.EvalKindBool:
		return val.Bool, nil
	case pipelineexpression.EvalKindNull:
		return false, nil
	default:
		return false, fmt.Errorf("filter expr expected BOOLEAN, got kind %v", val.Kind)
	}
}

// evalExprToAny parses+evaluates an expression and returns the native
// Go value. Used by project to compute each derived column.
func evalExprToAny(parsed pipelineexpression.Expr, row Row) (any, error) {
	exprRow, err := toExprRow(row)
	if err != nil {
		return nil, err
	}
	val, err := pipelineexpression.Eval(parsed, exprRow)
	if err != nil {
		return nil, err
	}
	return evalValueToAny(val), nil
}

// castValue converts `v` to the runtime representation matching the
// pipelineexpression.PipelineType target. Nil pass-through; values
// already in the target representation are returned unchanged.
// Returns a typed error when the conversion is not representable.
func castValue(v any, to pipelineexpression.PipelineType) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch to.Kind {
	case pipelineexpression.KindBoolean:
		return castToBool(v)
	case pipelineexpression.KindInteger, pipelineexpression.KindLong:
		i, err := castToInt64(v)
		if err != nil {
			return nil, err
		}
		return i, nil
	case pipelineexpression.KindDouble, pipelineexpression.KindDecimal:
		return castToFloat64(v)
	case pipelineexpression.KindString:
		return castToString(v), nil
	case pipelineexpression.KindDate, pipelineexpression.KindTimestamp:
		// Foundry's lattice stores dates / timestamps as strings on
		// the wire (ISO-8601). For v1 the cast is identity; the
		// runtime trusts the upstream column to already carry an
		// ISO-8601 representation. A proper Date/Timestamp parser
		// belongs in pipeline-expression and is tracked separately.
		return castToString(v), nil
	default:
		return nil, fmt.Errorf("cast to kind %q is not implemented in v1", to.Kind)
	}
}

func castToBool(v any) (bool, error) {
	switch x := v.(type) {
	case bool:
		return x, nil
	case string:
		b, err := strconv.ParseBool(x)
		if err != nil {
			return false, fmt.Errorf("cast %q to BOOLEAN: %w", x, err)
		}
		return b, nil
	case int, int32, int64, float32, float64:
		f, _ := castToFloat64(v)
		return f != 0, nil
	default:
		return false, fmt.Errorf("cast %T to BOOLEAN is unsupported", v)
	}
}

func castToInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case int64:
		return x, nil
	case float32:
		return int64(x), nil
	case float64:
		return int64(x), nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		i, err := strconv.ParseInt(x, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("cast %q to INTEGER/LONG: %w", x, err)
		}
		return i, nil
	case json.Number:
		return x.Int64()
	default:
		return 0, fmt.Errorf("cast %T to INTEGER/LONG is unsupported", v)
	}
}

func castToFloat64(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case float32:
		return float64(x), nil
	case int:
		return float64(x), nil
	case int32:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case bool:
		if x {
			return 1, nil
		}
		return 0, nil
	case string:
		f, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return 0, fmt.Errorf("cast %q to DOUBLE/DECIMAL: %w", x, err)
		}
		return f, nil
	case json.Number:
		return x.Float64()
	default:
		return 0, fmt.Errorf("cast %T to DOUBLE/DECIMAL is unsupported", v)
	}
}

func castToString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.FormatInt(int64(x), 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		b, _ := json.Marshal(v)
		s := string(b)
		// Strip surrounding quotes for bare JSON-string encodings.
		if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
			return s[1 : len(s)-1]
		}
		return s
	}
}
