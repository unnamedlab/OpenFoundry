// Comprehensive type-inference coverage for the Pipeline Builder
// expression DSL. Mirrors libs/pipeline-expression/tests/type_inference.rs
// 1:1 — same harness, same categories, same case names.
package pipelineexpression_test

import (
	"strings"
	"testing"

	pe "github.com/openfoundry/openfoundry-go/libs/pipeline-expression"
)

func envBasic() pe.ColumnEnv {
	return pe.NewColumnEnv().
		With("name", pe.StringType()).
		With("age", pe.IntegerType()).
		With("salary_long", pe.LongType()).
		With("salary_dbl", pe.DoubleType()).
		With("salary_dec", pe.DecimalType()).
		With("active", pe.BooleanType()).
		With("hired_on", pe.DateType()).
		With("last_seen", pe.TimestampType()).
		With("region", pe.GeometryType()).
		With("tags", pe.ArrayOf(pe.StringType()))
}

func assertOk(t *testing.T, expr string, env pe.ColumnEnv, expected pe.PipelineType) {
	t.Helper()
	parsed, err := pe.ParseExpr(expr)
	if err != nil {
		t.Fatalf("parse failed for `%s`: %v", expr, err)
	}
	got, errs := pe.InferExpr(parsed, env)
	if len(errs) > 0 {
		t.Fatalf("expected `%s` to type as %s, got errors %+v", expr, typeName(expected), errs)
	}
	if !got.Equal(expected) {
		t.Fatalf("expression `%s` typed as %s, want %s", expr, typeName(got), typeName(expected))
	}
}

func assertErrorCount(t *testing.T, expr string, env pe.ColumnEnv, n int) []pe.TypeError {
	t.Helper()
	parsed, err := pe.ParseExpr(expr)
	if err != nil {
		t.Fatalf("parse failed for `%s`: %v", expr, err)
	}
	_, errs := pe.InferExpr(parsed, env)
	if len(errs) != n {
		t.Fatalf("for `%s`: got %d errors, want %d (%+v)", expr, len(errs), n, errs)
	}
	return errs
}

func assertAnyError(t *testing.T, expr string, env pe.ColumnEnv) {
	t.Helper()
	parsed, err := pe.ParseExpr(expr)
	if err != nil {
		t.Fatalf("parse failed for `%s`: %v", expr, err)
	}
	_, errs := pe.InferExpr(parsed, env)
	if len(errs) == 0 {
		t.Fatalf("expected `%s` to fail type-checking but it succeeded", expr)
	}
}

// typeName mirrors the inner Rust helper for assertion messages.
func typeName(t pe.PipelineType) string {
	switch t.Kind {
	case pe.KindBoolean:
		return "Boolean"
	case pe.KindInteger:
		return "Integer"
	case pe.KindLong:
		return "Long"
	case pe.KindDouble:
		return "Double"
	case pe.KindDecimal:
		return "Decimal"
	case pe.KindString:
		return "String"
	case pe.KindDate:
		return "Date"
	case pe.KindTimestamp:
		return "Timestamp"
	case pe.KindGeometry:
		return "Geometry"
	case pe.KindArray:
		if t.Inner != nil {
			return "Array<" + typeName(*t.Inner) + ">"
		}
	case pe.KindStruct:
		var parts []string
		for _, f := range t.Fields {
			parts = append(parts, f.Name+": "+typeName(f.Type))
		}
		return "Struct<" + strings.Join(parts, ", ") + ">"
	}
	return ""
}

// ---------------------------------------------------------------------------
// Literals
// ---------------------------------------------------------------------------

func TestLiteralInteger(t *testing.T) {
	assertOk(t, "42", pe.NewColumnEnv(), pe.IntegerType())
}

func TestLiteralDouble(t *testing.T) {
	assertOk(t, "3.14", pe.NewColumnEnv(), pe.DoubleType())
}

func TestLiteralBoolTrue(t *testing.T) {
	assertOk(t, "true", pe.NewColumnEnv(), pe.BooleanType())
}

func TestLiteralBoolFalseCaseInsensitive(t *testing.T) {
	assertOk(t, "FALSE", pe.NewColumnEnv(), pe.BooleanType())
}

func TestLiteralStringSingleQuote(t *testing.T) {
	assertOk(t, "'hello'", pe.NewColumnEnv(), pe.StringType())
}

func TestLiteralStringDoubleQuote(t *testing.T) {
	assertOk(t, "\"hello\"", pe.NewColumnEnv(), pe.StringType())
}

func TestLiteralNullDefaultsToString(t *testing.T) {
	assertOk(t, "null", pe.NewColumnEnv(), pe.StringType())
}

// ---------------------------------------------------------------------------
// Column references
// ---------------------------------------------------------------------------

func TestColumnLookupString(t *testing.T) {
	assertOk(t, "name", envBasic(), pe.StringType())
}

func TestColumnLookupArray(t *testing.T) {
	assertOk(t, "tags", envBasic(), pe.ArrayOf(pe.StringType()))
}

func TestColumnUnknownReturnsError(t *testing.T) {
	errs := assertErrorCount(t, "missing", envBasic(), 1)
	if errs[0].Kind != pe.TypeErrUnknownColumn || errs[0].Detail != "missing" {
		t.Fatalf("got %+v, want UnknownColumn(missing)", errs[0])
	}
}

// ---------------------------------------------------------------------------
// Unary
// ---------------------------------------------------------------------------

func TestUnaryNegInteger(t *testing.T) {
	assertOk(t, "-age", envBasic(), pe.IntegerType())
}

func TestUnaryNegDouble(t *testing.T) {
	assertOk(t, "-salary_dbl", envBasic(), pe.DoubleType())
}

func TestUnaryNegStringRejected(t *testing.T) {
	assertAnyError(t, "-name", envBasic())
}

func TestUnaryNotBoolean(t *testing.T) {
	assertOk(t, "not active", envBasic(), pe.BooleanType())
}

func TestUnaryNotIntegerRejected(t *testing.T) {
	assertAnyError(t, "not age", envBasic())
}

// ---------------------------------------------------------------------------
// Numeric promotion
// ---------------------------------------------------------------------------

func TestAddIntIntIsInt(t *testing.T) {
	assertOk(t, "age + age", envBasic(), pe.IntegerType())
}

func TestAddIntLongIsLong(t *testing.T) {
	assertOk(t, "age + salary_long", envBasic(), pe.LongType())
}

func TestAddLongDoubleIsDouble(t *testing.T) {
	assertOk(t, "salary_long + salary_dbl", envBasic(), pe.DoubleType())
}

func TestAddDoubleDecimalIsDecimal(t *testing.T) {
	assertOk(t, "salary_dbl + salary_dec", envBasic(), pe.DecimalType())
}

func TestMixedArithPromotesThroughChain(t *testing.T) {
	assertOk(t, "age + salary_long + salary_dbl + salary_dec", envBasic(), pe.DecimalType())
}

func TestSubIntegerDoublePromotes(t *testing.T) {
	assertOk(t, "age - salary_dbl", envBasic(), pe.DoubleType())
}

func TestMulIntegerDecimal(t *testing.T) {
	assertOk(t, "age * salary_dec", envBasic(), pe.DecimalType())
}

func TestDivLongLong(t *testing.T) {
	assertOk(t, "salary_long / salary_long", envBasic(), pe.LongType())
}

func TestArithWithLiteralInteger(t *testing.T) {
	assertOk(t, "age * 2", envBasic(), pe.IntegerType())
}

func TestArithWithLiteralDouble(t *testing.T) {
	assertOk(t, "age * 2.5", envBasic(), pe.DoubleType())
}

// ---------------------------------------------------------------------------
// String concat via `+`
// ---------------------------------------------------------------------------

func TestAddStringStringIsString(t *testing.T) {
	assertOk(t, "name + name", envBasic(), pe.StringType())
}

func TestAddStringIntRejected(t *testing.T) {
	assertAnyError(t, "name + age", envBasic())
}

// ---------------------------------------------------------------------------
// Comparisons
// ---------------------------------------------------------------------------

func TestCmpIntInt(t *testing.T) {
	assertOk(t, "age > 18", envBasic(), pe.BooleanType())
}

func TestCmpIntLongPromotes(t *testing.T) {
	assertOk(t, "age = salary_long", envBasic(), pe.BooleanType())
}

func TestCmpStringString(t *testing.T) {
	assertOk(t, "name = 'Ada'", envBasic(), pe.BooleanType())
}

func TestCmpIntStringRejected(t *testing.T) {
	assertAnyError(t, "age = name", envBasic())
}

func TestCmpDateTimestampWidens(t *testing.T) {
	assertOk(t, "hired_on <= last_seen", envBasic(), pe.BooleanType())
}

func TestCmpGeometryStringRejected(t *testing.T) {
	assertAnyError(t, "region = 'POINT(0 0)'", envBasic())
}

// ---------------------------------------------------------------------------
// Logical
// ---------------------------------------------------------------------------

func TestAndTwoBool(t *testing.T) {
	assertOk(t, "active and age > 18", envBasic(), pe.BooleanType())
}

func TestOrTwoBool(t *testing.T) {
	assertOk(t, "active or age > 18", envBasic(), pe.BooleanType())
}

func TestAndWithIntRejected(t *testing.T) {
	assertAnyError(t, "active and age", envBasic())
}

// ---------------------------------------------------------------------------
// Function dispatch
// ---------------------------------------------------------------------------

func TestTitleCaseReturnsString(t *testing.T) {
	assertOk(t, "title_case(name)", envBasic(), pe.StringType())
}

func TestTitleCaseOnIntRejected(t *testing.T) {
	assertAnyError(t, "title_case(age)", envBasic())
}

func TestCleanStringOk(t *testing.T) {
	assertOk(t, "clean_string(name)", envBasic(), pe.StringType())
}

func TestLowerUpperTrimRoundTrip(t *testing.T) {
	assertOk(t, "lower(upper(trim(name)))", envBasic(), pe.StringType())
}

func TestConcatTwoStrings(t *testing.T) {
	assertOk(t, "concat(name, name)", envBasic(), pe.StringType())
}

func TestConcatArityError(t *testing.T) {
	errs := assertErrorCount(t, "concat(name)", envBasic(), 1)
	if errs[0].Kind != pe.TypeErrArity || errs[0].Name != "concat" || errs[0].ExpectedArity != 2 || errs[0].GotArity != 1 {
		t.Fatalf("got %+v, want Arity{name=concat, expected=2, got=1}", errs[0])
	}
}

func TestUnknownFunctionError(t *testing.T) {
	errs := assertErrorCount(t, "super_op(name)", envBasic(), 1)
	if errs[0].Kind != pe.TypeErrUnknownFunction || errs[0].Detail != "super_op" {
		t.Fatalf("got %+v, want UnknownFunction(super_op)", errs[0])
	}
}

func TestAbsReturnsInputTypeInt(t *testing.T) {
	assertOk(t, "abs(age)", envBasic(), pe.IntegerType())
}

func TestAbsReturnsInputTypeDouble(t *testing.T) {
	assertOk(t, "abs(salary_dbl)", envBasic(), pe.DoubleType())
}

func TestAbsOnStringRejected(t *testing.T) {
	assertAnyError(t, "abs(name)", envBasic())
}

func TestRoundPromotesLongInput(t *testing.T) {
	assertOk(t, "round(salary_long)", envBasic(), pe.LongType())
}

func TestToDateStringInputReturnsDate(t *testing.T) {
	assertOk(t, "to_date(name)", envBasic(), pe.DateType())
}

func TestToDateIntRejected(t *testing.T) {
	assertAnyError(t, "to_date(age)", envBasic())
}

func TestToTimestampReturnsTimestamp(t *testing.T) {
	assertOk(t, "to_timestamp(name)", envBasic(), pe.TimestampType())
}

func TestIsNullAnyArgReturnsBoolean(t *testing.T) {
	assertOk(t, "is_null(age)", envBasic(), pe.BooleanType())
	assertOk(t, "is_null(name)", envBasic(), pe.BooleanType())
}

func TestIsNotNullReturnsBoolean(t *testing.T) {
	assertOk(t, "is_not_null(active)", envBasic(), pe.BooleanType())
}

func TestGeomWithinTwoGeometries(t *testing.T) {
	assertOk(t, "geom_within(region, region)", envBasic(), pe.BooleanType())
}

func TestGeomWithinWithStringRejected(t *testing.T) {
	assertAnyError(t, "geom_within(region, name)", envBasic())
}

// ---------------------------------------------------------------------------
// Cast
// ---------------------------------------------------------------------------

func TestCastToLong(t *testing.T) {
	assertOk(t, "cast(age, 'LONG')", envBasic(), pe.LongType())
}

func TestCastToDecimal(t *testing.T) {
	assertOk(t, "cast(salary_long, 'DECIMAL')", envBasic(), pe.DecimalType())
}

func TestCastToArrayLong(t *testing.T) {
	assertOk(t, "cast(tags, 'ARRAY<LONG>')", envBasic(), pe.ArrayOf(pe.LongType()))
}

func TestCastToUnknownTypeRejected(t *testing.T) {
	errs := assertErrorCount(t, "cast(age, 'WHATEVER')", envBasic(), 1)
	if errs[0].Kind != pe.TypeErrInvalidCastTarget || errs[0].Detail != "WHATEVER" {
		t.Fatalf("got %+v, want InvalidCastTarget(WHATEVER)", errs[0])
	}
}

func TestCastTargetMustBeStringLiteral(t *testing.T) {
	errs := assertErrorCount(t, "cast(age, name)", envBasic(), 1)
	if errs[0].Kind != pe.TypeErrArgType {
		t.Fatalf("got %+v, want ArgType{...}", errs[0])
	}
}

func TestCastArityError(t *testing.T) {
	errs := assertErrorCount(t, "cast(age)", envBasic(), 1)
	if errs[0].Kind != pe.TypeErrArity {
		t.Fatalf("got %+v, want Arity{...}", errs[0])
	}
}

// ---------------------------------------------------------------------------
// Nested expressions / precedence
// ---------------------------------------------------------------------------

func TestNestedAndWithinOr(t *testing.T) {
	assertOk(t, "active and (age > 18 or salary_long > 100000)", envBasic(), pe.BooleanType())
}

func TestCastInsideArithmetic(t *testing.T) {
	assertOk(t, "cast(age, 'LONG') + salary_long", envBasic(), pe.LongType())
}

func TestFunctionInsideCmpInsideLogical(t *testing.T) {
	assertOk(t, "active and trim(name) = 'ada'", envBasic(), pe.BooleanType())
}

func TestPrecedenceMulBeforeAdd(t *testing.T) {
	assertOk(t, "age + age * 2", envBasic(), pe.IntegerType())
}

func TestParenthesisedArith(t *testing.T) {
	assertOk(t, "(age + 1) * 2", envBasic(), pe.IntegerType())
}

// ---------------------------------------------------------------------------
// Multiple errors in a single expression
// ---------------------------------------------------------------------------

func TestCollectsTwoUnknownColumns(t *testing.T) {
	errs := assertErrorCount(t, "foo + bar", envBasic(), 2)
	var names []string
	for _, e := range errs {
		if e.Kind == pe.TypeErrUnknownColumn {
			names = append(names, e.Detail)
		}
	}
	hasFoo, hasBar := false, false
	for _, n := range names {
		if n == "foo" {
			hasFoo = true
		}
		if n == "bar" {
			hasBar = true
		}
	}
	if !hasFoo || !hasBar {
		t.Fatalf("want unknown columns foo and bar, got %v", names)
	}
}

func TestCollectsUnknownColumnAndArityError(t *testing.T) {
	errs := assertErrorCount(t, "title_case(missing, missing2)", envBasic(), 1)
	// Arity error short-circuits before we descend into args.
	if errs[0].Kind != pe.TypeErrArity {
		t.Fatalf("got %+v, want Arity{...}", errs[0])
	}
}

// ---------------------------------------------------------------------------
// Promotion utility (direct)
// ---------------------------------------------------------------------------

func TestCanPromoteIntToLong(t *testing.T) {
	if !pe.CanPromote(pe.IntegerType(), pe.LongType()) {
		t.Fatal("CanPromote(Integer, Long) should be true")
	}
}

func TestCanPromoteLongToDecimal(t *testing.T) {
	if !pe.CanPromote(pe.LongType(), pe.DecimalType()) {
		t.Fatal("CanPromote(Long, Decimal) should be true")
	}
}

func TestCannotPromoteStringToInt(t *testing.T) {
	if pe.CanPromote(pe.StringType(), pe.IntegerType()) {
		t.Fatal("CanPromote(String, Integer) should be false")
	}
}

func TestCanPromoteArrayIntToArrayLong(t *testing.T) {
	if !pe.CanPromote(pe.ArrayOf(pe.IntegerType()), pe.ArrayOf(pe.LongType())) {
		t.Fatal("CanPromote(Array<Integer>, Array<Long>) should be true")
	}
}

func TestPromoteLubIntDoubleIsDouble(t *testing.T) {
	got, ok := pe.Promote(pe.IntegerType(), pe.DoubleType())
	if !ok || !got.Equal(pe.DoubleType()) {
		t.Fatalf("Promote(Integer, Double) = (%s, %v), want (Double, true)", typeName(got), ok)
	}
}

func TestPromoteDisjointReturnsNone(t *testing.T) {
	if _, ok := pe.Promote(pe.StringType(), pe.BooleanType()); ok {
		t.Fatal("Promote(String, Boolean) should return false")
	}
}

func TestPromoteDateTimestampReturnsTimestamp(t *testing.T) {
	got, ok := pe.Promote(pe.DateType(), pe.TimestampType())
	if !ok || !got.Equal(pe.TimestampType()) {
		t.Fatalf("Promote(Date, Timestamp) = (%s, %v), want (Timestamp, true)", typeName(got), ok)
	}
}
