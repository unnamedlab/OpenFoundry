//! FASE 3 — comprehensive type inference coverage for the Pipeline
//! Builder expression DSL. 50+ cases driven by a small test harness so
//! adding more cases stays cheap.
//!
//! Categories exercised:
//!
//! * literal typing (bool / int / double / string / null)
//! * column lookup, missing column, struct/array shapes
//! * unary & binary operator type rules
//! * numeric promotion (Integer → Long → Double → Decimal)
//! * Date → Timestamp implicit widening
//! * comparisons across families (rejected)
//! * call dispatch: title_case, clean_string, abs, concat, cast, is_null
//! * function arity errors
//! * cast target literal validation
//! * nested calls and operator precedence

use pipeline_expression::infer::TypeError;
use pipeline_expression::{ColumnEnv, PipelineType, infer_expr, parse_expr};

fn env_basic() -> ColumnEnv {
    ColumnEnv::new()
        .with("name", PipelineType::String)
        .with("age", PipelineType::Integer)
        .with("salary_long", PipelineType::Long)
        .with("salary_dbl", PipelineType::Double)
        .with("salary_dec", PipelineType::Decimal)
        .with("active", PipelineType::Boolean)
        .with("hired_on", PipelineType::Date)
        .with("last_seen", PipelineType::Timestamp)
        .with("region", PipelineType::Geometry)
        .with(
            "tags",
            PipelineType::array_of(PipelineType::String),
        )
}

fn assert_ok(expr: &str, env: &ColumnEnv, expected: PipelineType) {
    let parsed = parse_expr(expr).unwrap_or_else(|e| panic!("parse failed for `{expr}`: {e:?}"));
    match infer_expr(&parsed, env) {
        Ok(t) => assert_eq!(t, expected, "expression `{expr}` typed as {t:?}"),
        Err(errs) => panic!("expected `{expr}` to type as {expected:?}, got errors {errs:?}"),
    }
}

fn assert_error_count(expr: &str, env: &ColumnEnv, n: usize) -> Vec<TypeError> {
    let parsed = parse_expr(expr).unwrap_or_else(|e| panic!("parse failed for `{expr}`: {e:?}"));
    let errs = infer_expr(&parsed, env).unwrap_err();
    assert_eq!(errs.len(), n, "for `{expr}`: {errs:?}");
    errs
}

fn assert_any_error(expr: &str, env: &ColumnEnv) {
    let parsed = parse_expr(expr).unwrap_or_else(|e| panic!("parse failed for `{expr}`: {e:?}"));
    assert!(
        infer_expr(&parsed, env).is_err(),
        "expected `{expr}` to fail type-checking but it succeeded"
    );
}

// ---------------------------------------------------------------------------
// Literals
// ---------------------------------------------------------------------------

#[test]
fn literal_integer() {
    assert_ok("42", &ColumnEnv::new(), PipelineType::Integer);
}

#[test]
fn literal_double() {
    assert_ok("3.14", &ColumnEnv::new(), PipelineType::Double);
}

#[test]
fn literal_bool_true() {
    assert_ok("true", &ColumnEnv::new(), PipelineType::Boolean);
}

#[test]
fn literal_bool_false_case_insensitive() {
    assert_ok("FALSE", &ColumnEnv::new(), PipelineType::Boolean);
}

#[test]
fn literal_string_single_quote() {
    assert_ok("'hello'", &ColumnEnv::new(), PipelineType::String);
}

#[test]
fn literal_string_double_quote() {
    assert_ok("\"hello\"", &ColumnEnv::new(), PipelineType::String);
}

#[test]
fn literal_null_defaults_to_string() {
    assert_ok("null", &ColumnEnv::new(), PipelineType::String);
}

// ---------------------------------------------------------------------------
// Column references
// ---------------------------------------------------------------------------

#[test]
fn column_lookup_string() {
    assert_ok("name", &env_basic(), PipelineType::String);
}

#[test]
fn column_lookup_array() {
    assert_ok("tags", &env_basic(), PipelineType::array_of(PipelineType::String));
}

#[test]
fn column_unknown_returns_error() {
    let errs = assert_error_count("missing", &env_basic(), 1);
    assert!(matches!(errs[0], TypeError::UnknownColumn(ref n) if n == "missing"));
}

// ---------------------------------------------------------------------------
// Unary
// ---------------------------------------------------------------------------

#[test]
fn unary_neg_integer() {
    assert_ok("-age", &env_basic(), PipelineType::Integer);
}

#[test]
fn unary_neg_double() {
    assert_ok("-salary_dbl", &env_basic(), PipelineType::Double);
}

#[test]
fn unary_neg_string_rejected() {
    assert_any_error("-name", &env_basic());
}

#[test]
fn unary_not_boolean() {
    assert_ok("not active", &env_basic(), PipelineType::Boolean);
}

#[test]
fn unary_not_integer_rejected() {
    assert_any_error("not age", &env_basic());
}

// ---------------------------------------------------------------------------
// Numeric promotion
// ---------------------------------------------------------------------------

#[test]
fn add_int_int_is_int() {
    assert_ok("age + age", &env_basic(), PipelineType::Integer);
}

#[test]
fn add_int_long_is_long() {
    assert_ok("age + salary_long", &env_basic(), PipelineType::Long);
}

#[test]
fn add_long_double_is_double() {
    assert_ok("salary_long + salary_dbl", &env_basic(), PipelineType::Double);
}

#[test]
fn add_double_decimal_is_decimal() {
    assert_ok("salary_dbl + salary_dec", &env_basic(), PipelineType::Decimal);
}

#[test]
fn mixed_arith_promotes_through_chain() {
    assert_ok(
        "age + salary_long + salary_dbl + salary_dec",
        &env_basic(),
        PipelineType::Decimal,
    );
}

#[test]
fn sub_integer_double_promotes() {
    assert_ok("age - salary_dbl", &env_basic(), PipelineType::Double);
}

#[test]
fn mul_integer_decimal() {
    assert_ok("age * salary_dec", &env_basic(), PipelineType::Decimal);
}

#[test]
fn div_long_long() {
    assert_ok("salary_long / salary_long", &env_basic(), PipelineType::Long);
}

#[test]
fn arith_with_literal_integer() {
    assert_ok("age * 2", &env_basic(), PipelineType::Integer);
}

#[test]
fn arith_with_literal_double() {
    assert_ok("age * 2.5", &env_basic(), PipelineType::Double);
}

// ---------------------------------------------------------------------------
// String concat via `+`
// ---------------------------------------------------------------------------

#[test]
fn add_string_string_is_string() {
    assert_ok("name + name", &env_basic(), PipelineType::String);
}

#[test]
fn add_string_int_rejected() {
    assert_any_error("name + age", &env_basic());
}

// ---------------------------------------------------------------------------
// Comparisons
// ---------------------------------------------------------------------------

#[test]
fn cmp_int_int() {
    assert_ok("age > 18", &env_basic(), PipelineType::Boolean);
}

#[test]
fn cmp_int_long_promotes() {
    assert_ok("age = salary_long", &env_basic(), PipelineType::Boolean);
}

#[test]
fn cmp_string_string() {
    assert_ok("name = 'Ada'", &env_basic(), PipelineType::Boolean);
}

#[test]
fn cmp_int_string_rejected() {
    assert_any_error("age = name", &env_basic());
}

#[test]
fn cmp_date_timestamp_widens() {
    assert_ok("hired_on <= last_seen", &env_basic(), PipelineType::Boolean);
}

#[test]
fn cmp_geometry_string_rejected() {
    assert_any_error("region = 'POINT(0 0)'", &env_basic());
}

// ---------------------------------------------------------------------------
// Logical
// ---------------------------------------------------------------------------

#[test]
fn and_two_bool() {
    assert_ok(
        "active and age > 18",
        &env_basic(),
        PipelineType::Boolean,
    );
}

#[test]
fn or_two_bool() {
    assert_ok(
        "active or age > 18",
        &env_basic(),
        PipelineType::Boolean,
    );
}

#[test]
fn and_with_int_rejected() {
    assert_any_error("active and age", &env_basic());
}

// ---------------------------------------------------------------------------
// Function dispatch
// ---------------------------------------------------------------------------

#[test]
fn title_case_returns_string() {
    assert_ok("title_case(name)", &env_basic(), PipelineType::String);
}

#[test]
fn title_case_on_int_rejected() {
    assert_any_error("title_case(age)", &env_basic());
}

#[test]
fn clean_string_ok() {
    assert_ok("clean_string(name)", &env_basic(), PipelineType::String);
}

#[test]
fn lower_upper_trim_round_trip() {
    assert_ok(
        "lower(upper(trim(name)))",
        &env_basic(),
        PipelineType::String,
    );
}

#[test]
fn concat_two_strings() {
    assert_ok(
        "concat(name, name)",
        &env_basic(),
        PipelineType::String,
    );
}

#[test]
fn concat_arity_error() {
    let errs = assert_error_count("concat(name)", &env_basic(), 1);
    assert!(matches!(
        errs[0],
        TypeError::Arity { ref name, expected: 2, got: 1 } if name == "concat"
    ));
}

#[test]
fn unknown_function_error() {
    let errs = assert_error_count("super_op(name)", &env_basic(), 1);
    assert!(matches!(errs[0], TypeError::UnknownFunction(ref n) if n == "super_op"));
}

#[test]
fn abs_returns_input_type_int() {
    assert_ok("abs(age)", &env_basic(), PipelineType::Integer);
}

#[test]
fn abs_returns_input_type_double() {
    assert_ok("abs(salary_dbl)", &env_basic(), PipelineType::Double);
}

#[test]
fn abs_on_string_rejected() {
    assert_any_error("abs(name)", &env_basic());
}

#[test]
fn round_promotes_long_input() {
    assert_ok("round(salary_long)", &env_basic(), PipelineType::Long);
}

#[test]
fn to_date_string_input_returns_date() {
    assert_ok("to_date(name)", &env_basic(), PipelineType::Date);
}

#[test]
fn to_date_int_rejected() {
    assert_any_error("to_date(age)", &env_basic());
}

#[test]
fn to_timestamp_returns_timestamp() {
    assert_ok("to_timestamp(name)", &env_basic(), PipelineType::Timestamp);
}

#[test]
fn is_null_any_arg_returns_boolean() {
    assert_ok("is_null(age)", &env_basic(), PipelineType::Boolean);
    assert_ok("is_null(name)", &env_basic(), PipelineType::Boolean);
}

#[test]
fn is_not_null_returns_boolean() {
    assert_ok("is_not_null(active)", &env_basic(), PipelineType::Boolean);
}

#[test]
fn geom_within_two_geometries() {
    assert_ok(
        "geom_within(region, region)",
        &env_basic(),
        PipelineType::Boolean,
    );
}

#[test]
fn geom_within_with_string_rejected() {
    assert_any_error("geom_within(region, name)", &env_basic());
}

// ---------------------------------------------------------------------------
// Cast
// ---------------------------------------------------------------------------

#[test]
fn cast_to_long() {
    assert_ok("cast(age, 'LONG')", &env_basic(), PipelineType::Long);
}

#[test]
fn cast_to_decimal() {
    assert_ok(
        "cast(salary_long, 'DECIMAL')",
        &env_basic(),
        PipelineType::Decimal,
    );
}

#[test]
fn cast_to_array_long() {
    assert_ok(
        "cast(tags, 'ARRAY<LONG>')",
        &env_basic(),
        PipelineType::array_of(PipelineType::Long),
    );
}

#[test]
fn cast_to_unknown_type_rejected() {
    let errs = assert_error_count("cast(age, 'WHATEVER')", &env_basic(), 1);
    assert!(matches!(errs[0], TypeError::InvalidCastTarget(ref s) if s == "WHATEVER"));
}

#[test]
fn cast_target_must_be_string_literal() {
    let errs = assert_error_count("cast(age, name)", &env_basic(), 1);
    assert!(matches!(errs[0], TypeError::ArgType { .. }));
}

#[test]
fn cast_arity_error() {
    let errs = assert_error_count("cast(age)", &env_basic(), 1);
    assert!(matches!(errs[0], TypeError::Arity { .. }));
}

// ---------------------------------------------------------------------------
// Nested expressions / precedence
// ---------------------------------------------------------------------------

#[test]
fn nested_and_within_or() {
    assert_ok(
        "active and (age > 18 or salary_long > 100000)",
        &env_basic(),
        PipelineType::Boolean,
    );
}

#[test]
fn cast_inside_arithmetic() {
    assert_ok(
        "cast(age, 'LONG') + salary_long",
        &env_basic(),
        PipelineType::Long,
    );
}

#[test]
fn function_inside_cmp_inside_logical() {
    assert_ok(
        "active and trim(name) = 'ada'",
        &env_basic(),
        PipelineType::Boolean,
    );
}

#[test]
fn precedence_mul_before_add() {
    assert_ok("age + age * 2", &env_basic(), PipelineType::Integer);
}

#[test]
fn parenthesised_arith() {
    assert_ok("(age + 1) * 2", &env_basic(), PipelineType::Integer);
}

// ---------------------------------------------------------------------------
// Multiple errors in a single expression
// ---------------------------------------------------------------------------

#[test]
fn collects_two_unknown_columns() {
    let errs = assert_error_count("foo + bar", &env_basic(), 2);
    let names: Vec<&str> = errs
        .iter()
        .filter_map(|e| match e {
            TypeError::UnknownColumn(n) => Some(n.as_str()),
            _ => None,
        })
        .collect();
    assert!(names.contains(&"foo") && names.contains(&"bar"));
}

#[test]
fn collects_unknown_column_and_arity_error() {
    let errs = assert_error_count("title_case(missing, missing2)", &env_basic(), 1);
    // Arity error short-circuits before we descend into args.
    assert!(matches!(errs[0], TypeError::Arity { .. }));
}

// ---------------------------------------------------------------------------
// Promotion utility (direct)
// ---------------------------------------------------------------------------

#[test]
fn can_promote_int_to_long() {
    assert!(pipeline_expression::can_promote(
        &PipelineType::Integer,
        &PipelineType::Long,
    ));
}

#[test]
fn can_promote_long_to_decimal() {
    assert!(pipeline_expression::can_promote(
        &PipelineType::Long,
        &PipelineType::Decimal,
    ));
}

#[test]
fn cannot_promote_string_to_int() {
    assert!(!pipeline_expression::can_promote(
        &PipelineType::String,
        &PipelineType::Integer,
    ));
}

#[test]
fn can_promote_array_int_to_array_long() {
    assert!(pipeline_expression::can_promote(
        &PipelineType::array_of(PipelineType::Integer),
        &PipelineType::array_of(PipelineType::Long),
    ));
}

#[test]
fn promote_lub_int_double_is_double() {
    assert_eq!(
        pipeline_expression::promote(
            &PipelineType::Integer,
            &PipelineType::Double,
        ),
        Some(PipelineType::Double),
    );
}

#[test]
fn promote_disjoint_returns_none() {
    assert!(pipeline_expression::promote(
        &PipelineType::String,
        &PipelineType::Boolean,
    )
    .is_none());
}

#[test]
fn promote_date_timestamp_returns_timestamp() {
    assert_eq!(
        pipeline_expression::promote(&PipelineType::Date, &PipelineType::Timestamp),
        Some(PipelineType::Timestamp),
    );
}
