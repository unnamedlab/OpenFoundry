package pipelineexpression

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ColumnEnv binds column names to their inferred [PipelineType].
type ColumnEnv struct {
	columns map[string]PipelineType
}

// NewColumnEnv builds an empty environment.
func NewColumnEnv() ColumnEnv {
	return ColumnEnv{columns: map[string]PipelineType{}}
}

// With returns a copy of the environment with `name` bound to `ty`.
// Mirrors the chainable Rust builder.
func (e ColumnEnv) With(name string, ty PipelineType) ColumnEnv {
	out := ColumnEnv{columns: make(map[string]PipelineType, len(e.columns)+1)}
	for k, v := range e.columns {
		out.columns[k] = v
	}
	out.columns[name] = ty
	return out
}

// Insert mutates the receiver, binding `name` to `ty`.
func (e *ColumnEnv) Insert(name string, ty PipelineType) {
	if e.columns == nil {
		e.columns = map[string]PipelineType{}
	}
	e.columns[name] = ty
}

// Lookup returns the type bound to `name` and whether it was present.
func (e ColumnEnv) Lookup(name string) (PipelineType, bool) {
	t, ok := e.columns[name]
	return t, ok
}

// Len returns the number of bindings.
func (e ColumnEnv) Len() int {
	return len(e.columns)
}

// IsEmpty reports whether the environment has no bindings.
func (e ColumnEnv) IsEmpty() bool {
	return len(e.columns) == 0
}

// TypeErrorKind tags a [TypeError].
type TypeErrorKind string

const (
	// TypeErrUnknownColumn — column referenced but absent from the env.
	TypeErrUnknownColumn TypeErrorKind = "UnknownColumn"
	// TypeErrUnknownFunction — call to a function the catalog ignores.
	TypeErrUnknownFunction TypeErrorKind = "UnknownFunction"
	// TypeErrArity — function called with the wrong number of args.
	TypeErrArity TypeErrorKind = "Arity"
	// TypeErrArgType — argument fails its constraint.
	TypeErrArgType TypeErrorKind = "ArgType"
	// TypeErrBinaryOp — binary operator not defined for the operand types.
	TypeErrBinaryOp TypeErrorKind = "BinaryOp"
	// TypeErrUnaryOp — unary operator not defined for the operand type.
	TypeErrUnaryOp TypeErrorKind = "UnaryOp"
	// TypeErrInvalidCastTarget — cast target literal is not a known type.
	TypeErrInvalidCastTarget TypeErrorKind = "InvalidCastTarget"
)

// TypeError describes a single type-checking failure. Mirrors the
// Rust thiserror enum.
type TypeError struct {
	Kind TypeErrorKind
	// Common payload.
	Name     string
	Expected string
	Got      string
	Index    int
	// BinaryOp.
	Op    string
	Left  string
	Right string
	// UnaryOp.
	Type string
	// Arity counts.
	ExpectedArity int
	GotArity      int
	// Generic single-string variants.
	Detail string
}

// Error implements [error]. Mirrors the Rust thiserror messages.
func (e TypeError) Error() string {
	switch e.Kind {
	case TypeErrUnknownColumn:
		return fmt.Sprintf("unknown column '%s'", e.Detail)
	case TypeErrUnknownFunction:
		return fmt.Sprintf("unknown function '%s'", e.Detail)
	case TypeErrArity:
		return fmt.Sprintf("function '%s' expects %d args, got %d", e.Name, e.ExpectedArity, e.GotArity)
	case TypeErrArgType:
		return fmt.Sprintf("function '%s' arg %d: expected %s, got %s", e.Name, e.Index, e.Expected, e.Got)
	case TypeErrBinaryOp:
		return fmt.Sprintf("operator '%s' is not defined for %s and %s", e.Op, e.Left, e.Right)
	case TypeErrUnaryOp:
		return fmt.Sprintf("operator '%s' is not defined for %s", e.Op, e.Type)
	case TypeErrInvalidCastTarget:
		return fmt.Sprintf("cast target literal must be a known type, got '%s'", e.Detail)
	}
	return "unknown type error"
}

// MarshalJSON mirrors the Rust serde shape `{kind, detail}` where the
// detail payload depends on the variant.
func (e TypeError) MarshalJSON() ([]byte, error) {
	type out struct {
		Kind   TypeErrorKind `json:"kind"`
		Detail any           `json:"detail"`
	}
	switch e.Kind {
	case TypeErrUnknownColumn, TypeErrUnknownFunction, TypeErrInvalidCastTarget:
		return json.Marshal(out{Kind: e.Kind, Detail: e.Detail})
	case TypeErrArity:
		return json.Marshal(out{Kind: e.Kind, Detail: struct {
			Name     string `json:"name"`
			Expected int    `json:"expected"`
			Got      int    `json:"got"`
		}{Name: e.Name, Expected: e.ExpectedArity, Got: e.GotArity}})
	case TypeErrArgType:
		return json.Marshal(out{Kind: e.Kind, Detail: struct {
			Name     string `json:"name"`
			Index    int    `json:"index"`
			Expected string `json:"expected"`
			Got      string `json:"got"`
		}{Name: e.Name, Index: e.Index, Expected: e.Expected, Got: e.Got}})
	case TypeErrBinaryOp:
		return json.Marshal(out{Kind: e.Kind, Detail: struct {
			Op    string `json:"op"`
			Left  string `json:"left"`
			Right string `json:"right"`
		}{Op: e.Op, Left: e.Left, Right: e.Right}})
	case TypeErrUnaryOp:
		return json.Marshal(out{Kind: e.Kind, Detail: struct {
			Op string `json:"op"`
			Ty string `json:"ty"`
		}{Op: e.Op, Ty: e.Type}})
	}
	return nil, fmt.Errorf("pipelineexpression: unknown TypeError kind %q", e.Kind)
}

// InferExpr returns the inferred [PipelineType] for `expr` under
// `env`. When type-checking fails, the returned error slice describes
// every offending site. Mirrors the Rust fail-fast convenience —
// callers get a fully-typed expression or the full list of errors.
//
// When the expression types successfully but [inferInner] returns no
// concrete inference (e.g. unknown column with the rest of the tree
// unreachable), we fall back to [StringType] to match the Rust
// behaviour.
func InferExpr(expr Expr, env ColumnEnv) (PipelineType, []TypeError) {
	var errors []TypeError
	inferred, ok := inferInner(expr, env, &errors)
	if len(errors) > 0 {
		return PipelineType{}, errors
	}
	if !ok {
		return StringType(), nil
	}
	return inferred, nil
}

func inferInner(expr Expr, env ColumnEnv, errors *[]TypeError) (PipelineType, bool) {
	switch expr.Kind {
	case ExprLit:
		return litType(expr.Lit), true
	case ExprColumn:
		t, ok := env.Lookup(expr.Name)
		if !ok {
			*errors = append(*errors, TypeError{
				Kind:   TypeErrUnknownColumn,
				Detail: expr.Name,
			})
			return PipelineType{}, false
		}
		return t, true
	case ExprUnary:
		if expr.Operand == nil {
			return PipelineType{}, false
		}
		inner, ok := inferInner(*expr.Operand, env, errors)
		if !ok {
			return PipelineType{}, false
		}
		switch expr.UnaryOp {
		case UnaryNeg:
			if inner.IsNumeric() {
				return inner, true
			}
			*errors = append(*errors, TypeError{
				Kind: TypeErrUnaryOp,
				Op:   "-",
				Type: typeName(inner),
			})
			return PipelineType{}, false
		case UnaryNot:
			if inner.Kind == KindBoolean {
				return BooleanType(), true
			}
			*errors = append(*errors, TypeError{
				Kind: TypeErrUnaryOp,
				Op:   "not",
				Type: typeName(inner),
			})
			return PipelineType{}, false
		}
		return PipelineType{}, false
	case ExprBinary:
		if expr.Left == nil || expr.Right == nil {
			return PipelineType{}, false
		}
		l, lOk := inferInner(*expr.Left, env, errors)
		r, rOk := inferInner(*expr.Right, env, errors)
		if !lOk || !rOk {
			return PipelineType{}, false
		}
		return typeBinary(expr.BinaryOp, l, r, errors)
	case ExprCall:
		return inferCall(expr.Name, expr.Args, env, errors)
	}
	return PipelineType{}, false
}

func typeBinary(op BinaryOp, l, r PipelineType, errors *[]TypeError) (PipelineType, bool) {
	pushErr := func() {
		*errors = append(*errors, TypeError{
			Kind:  TypeErrBinaryOp,
			Op:    op.String(),
			Left:  typeName(l),
			Right: typeName(r),
		})
	}

	if op.IsLogical() {
		if l.Kind == KindBoolean && r.Kind == KindBoolean {
			return BooleanType(), true
		}
		pushErr()
		return PipelineType{}, false
	}

	if op.IsComparison() {
		ok := (l.IsNumeric() && r.IsNumeric()) ||
			(l.IsTextual() && r.IsTextual()) ||
			(l.IsTemporal() && r.IsTemporal()) ||
			l.Equal(r)
		if ok {
			return BooleanType(), true
		}
		pushErr()
		return PipelineType{}, false
	}

	switch op {
	case BinaryAdd, BinarySub, BinaryMul, BinaryDiv:
		if l.IsNumeric() && r.IsNumeric() {
			t, ok := Promote(l, r)
			if !ok {
				return PipelineType{}, false
			}
			return t, true
		}
		if op == BinaryAdd && l.IsTextual() && r.IsTextual() {
			return StringType(), true
		}
		pushErr()
		return PipelineType{}, false
	}
	return PipelineType{}, false
}

func inferCall(name string, args []Expr, env ColumnEnv, errors *[]TypeError) (PipelineType, bool) {
	sig, ok := ScalarSignatureFor(name)
	if !ok {
		*errors = append(*errors, TypeError{
			Kind:   TypeErrUnknownFunction,
			Detail: name,
		})
		return PipelineType{}, false
	}

	if len(args) != len(sig.Params) {
		*errors = append(*errors, TypeError{
			Kind:          TypeErrArity,
			Name:          name,
			ExpectedArity: len(sig.Params),
			GotArity:      len(args),
		})
		return PipelineType{}, false
	}

	type argInfo struct {
		ty PipelineType
		ok bool
	}
	argTypes := make([]argInfo, len(args))
	for i, arg := range args {
		t, tOk := inferInner(arg, env, errors)
		if tOk {
			checkConstraint(name, i, t, sig.Params[i], errors)
		}
		argTypes[i] = argInfo{ty: t, ok: tOk}
	}

	switch sig.Result.Kind {
	case ResultFixed:
		return sig.Result.Type, true
	case ResultPromoteOf:
		var out PipelineType
		haveOut := false
		for _, idx := range sig.Result.Args {
			if idx < 0 || idx >= len(argTypes) {
				continue
			}
			if !argTypes[idx].ok {
				continue
			}
			if !haveOut {
				out = argTypes[idx].ty
				haveOut = true
				continue
			}
			t, ok := Promote(out, argTypes[idx].ty)
			if !ok {
				return PipelineType{}, false
			}
			out = t
		}
		if !haveOut {
			return PipelineType{}, false
		}
		return out, true
	case ResultTypeFromStringArg:
		idx := sig.Result.Index
		if idx < 0 || idx >= len(args) {
			*errors = append(*errors, TypeError{
				Kind:     TypeErrArgType,
				Name:     name,
				Index:    idx,
				Expected: "STRING literal",
				Got:      "expression",
			})
			return PipelineType{}, false
		}
		argExpr := args[idx]
		if argExpr.Kind != ExprLit || argExpr.Lit.Kind != LitString {
			*errors = append(*errors, TypeError{
				Kind:     TypeErrArgType,
				Name:     name,
				Index:    idx,
				Expected: "STRING literal",
				Got:      "expression",
			})
			return PipelineType{}, false
		}
		t, ok := ParseTypeLiteral(argExpr.Lit.Str)
		if !ok {
			*errors = append(*errors, TypeError{
				Kind:   TypeErrInvalidCastTarget,
				Detail: argExpr.Lit.Str,
			})
			return PipelineType{}, false
		}
		return t, true
	}
	return PipelineType{}, false
}

func checkConstraint(name string, index int, actual PipelineType, constraint ParamConstraint, errors *[]TypeError) {
	push := func(expected string) {
		*errors = append(*errors, TypeError{
			Kind:     TypeErrArgType,
			Name:     name,
			Index:    index,
			Expected: expected,
			Got:      typeName(actual),
		})
	}
	switch constraint.Kind {
	case ParamAny:
		// no-op
	case ParamExactly:
		if !actual.Equal(constraint.Type) {
			push(typeName(constraint.Type))
		}
	case ParamPromotable:
		if !CanPromote(actual, constraint.Type) {
			push(fmt.Sprintf("promotable to %s", typeName(constraint.Type)))
		}
	case ParamNumeric:
		if !actual.IsNumeric() {
			push("numeric")
		}
	case ParamTextual:
		if !actual.IsTextual() {
			push("string")
		}
	case ParamTemporal:
		if !actual.IsTemporal() {
			push("date or timestamp")
		}
	}
}

func litType(lit Literal) PipelineType {
	switch lit.Kind {
	case LitBool:
		return BooleanType()
	case LitInteger:
		return IntegerType()
	case LitDouble:
		return DoubleType()
	case LitString:
		return StringType()
	}
	// Untyped null defaults to String — most pipeline columns are
	// string-shaped at the source, and a nullable string is the safest
	// default. Callers who care can `cast(null, "INTEGER")`.
	return StringType()
}

func typeName(t PipelineType) string {
	switch t.Kind {
	case KindBoolean:
		return "Boolean"
	case KindInteger:
		return "Integer"
	case KindLong:
		return "Long"
	case KindDouble:
		return "Double"
	case KindDecimal:
		return "Decimal"
	case KindString:
		return "String"
	case KindDate:
		return "Date"
	case KindTimestamp:
		return "Timestamp"
	case KindGeometry:
		return "Geometry"
	case KindArray:
		if t.Inner == nil {
			return "Array<?>"
		}
		return fmt.Sprintf("Array<%s>", typeName(*t.Inner))
	case KindStruct:
		parts := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			parts[i] = fmt.Sprintf("%s: %s", f.Name, typeName(f.Type))
		}
		return fmt.Sprintf("Struct<%s>", strings.Join(parts, ", "))
	}
	return ""
}
