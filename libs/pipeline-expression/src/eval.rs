//! Row-level evaluator for parsed [`Expr`](crate::parser::Expr) trees.
//!
//! Pure-Rust, no external deps. Designed to be fast enough for the
//! ~50 K-row preview sample size: the AST is walked once per row.
//! Cross-row aggregations (group_by, window) are handled at a higher
//! layer — this module evaluates scalar / row-local expressions only.
//!
//! Mirrors the shape of the type checker in [`crate::infer`]: the same
//! lattice, the same operators. When a value is missing or the wrong
//! shape, we surface an [`EvalError`] rather than panicking.

use std::collections::HashMap;

use serde_json::Value as Json;

use crate::parser::{BinaryOp, Expr, Literal, UnaryOp};
use crate::types::PipelineType;

pub type Row = HashMap<String, Json>;

#[derive(Debug, Clone, PartialEq)]
pub enum EvalValue {
    Bool(bool),
    Integer(i64),
    Double(f64),
    String(String),
    Null,
}

impl EvalValue {
    pub fn from_json(v: &Json) -> Self {
        match v {
            Json::Null => EvalValue::Null,
            Json::Bool(b) => EvalValue::Bool(*b),
            Json::Number(n) => {
                if let Some(i) = n.as_i64() {
                    EvalValue::Integer(i)
                } else {
                    EvalValue::Double(n.as_f64().unwrap_or(0.0))
                }
            }
            Json::String(s) => EvalValue::String(s.clone()),
            _ => EvalValue::Null,
        }
    }

    pub fn to_json(&self) -> Json {
        match self {
            EvalValue::Bool(b) => Json::Bool(*b),
            EvalValue::Integer(i) => Json::from(*i),
            EvalValue::Double(d) => serde_json::Number::from_f64(*d)
                .map(Json::Number)
                .unwrap_or(Json::Null),
            EvalValue::String(s) => Json::String(s.clone()),
            EvalValue::Null => Json::Null,
        }
    }

    pub fn as_bool(&self) -> Option<bool> {
        if let EvalValue::Bool(b) = self {
            Some(*b)
        } else {
            None
        }
    }

    pub fn type_hint(&self) -> PipelineType {
        match self {
            EvalValue::Bool(_) => PipelineType::Boolean,
            EvalValue::Integer(_) => PipelineType::Integer,
            EvalValue::Double(_) => PipelineType::Double,
            EvalValue::String(_) => PipelineType::String,
            EvalValue::Null => PipelineType::String,
        }
    }
}

#[derive(Debug, Clone, PartialEq)]
pub enum EvalError {
    UnknownColumn(String),
    UnknownFunction(String),
    Arity { name: String, expected: usize, got: usize },
    TypeMismatch(String),
}

impl std::fmt::Display for EvalError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            EvalError::UnknownColumn(c) => write!(f, "unknown column '{c}'"),
            EvalError::UnknownFunction(c) => write!(f, "unknown function '{c}'"),
            EvalError::Arity { name, expected, got } => {
                write!(f, "function '{name}' expects {expected} args, got {got}")
            }
            EvalError::TypeMismatch(s) => write!(f, "{s}"),
        }
    }
}

impl std::error::Error for EvalError {}

pub fn eval(expr: &Expr, row: &Row) -> Result<EvalValue, EvalError> {
    match expr {
        Expr::Lit(lit) => Ok(eval_lit(lit)),
        Expr::Column(name) => row
            .get(name)
            .map(EvalValue::from_json)
            .ok_or_else(|| EvalError::UnknownColumn(name.clone())),
        Expr::Unary { op, operand } => {
            let inner = eval(operand, row)?;
            match (op, &inner) {
                (UnaryOp::Neg, EvalValue::Integer(i)) => Ok(EvalValue::Integer(-i)),
                (UnaryOp::Neg, EvalValue::Double(d)) => Ok(EvalValue::Double(-d)),
                (UnaryOp::Neg, _) => Err(EvalError::TypeMismatch(format!(
                    "unary '-' not defined for {:?}",
                    inner.type_hint()
                ))),
                (UnaryOp::Not, EvalValue::Bool(b)) => Ok(EvalValue::Bool(!b)),
                (UnaryOp::Not, _) => Err(EvalError::TypeMismatch(format!(
                    "unary 'not' not defined for {:?}",
                    inner.type_hint()
                ))),
            }
        }
        Expr::Binary { op, left, right } => eval_binary(*op, left, right, row),
        Expr::Call { name, args } => eval_call(name, args, row),
    }
}

fn eval_lit(lit: &Literal) -> EvalValue {
    match lit {
        Literal::Bool(b) => EvalValue::Bool(*b),
        Literal::Integer(i) => EvalValue::Integer(*i),
        Literal::Double(d) => EvalValue::Double(*d),
        Literal::String(s) => EvalValue::String(s.clone()),
        Literal::Null => EvalValue::Null,
    }
}

fn eval_binary(
    op: BinaryOp,
    left: &Expr,
    right: &Expr,
    row: &Row,
) -> Result<EvalValue, EvalError> {
    let l = eval(left, row)?;
    let r = eval(right, row)?;
    match op {
        BinaryOp::And => match (&l, &r) {
            (EvalValue::Bool(a), EvalValue::Bool(b)) => Ok(EvalValue::Bool(*a && *b)),
            _ => Err(type_mismatch("and", &l, &r)),
        },
        BinaryOp::Or => match (&l, &r) {
            (EvalValue::Bool(a), EvalValue::Bool(b)) => Ok(EvalValue::Bool(*a || *b)),
            _ => Err(type_mismatch("or", &l, &r)),
        },
        BinaryOp::Eq => Ok(EvalValue::Bool(value_eq(&l, &r))),
        BinaryOp::NotEq => Ok(EvalValue::Bool(!value_eq(&l, &r))),
        BinaryOp::Lt => compare(&l, &r).map(|o| EvalValue::Bool(o == std::cmp::Ordering::Less)),
        BinaryOp::Lte => compare(&l, &r).map(|o| {
            EvalValue::Bool(o == std::cmp::Ordering::Less || o == std::cmp::Ordering::Equal)
        }),
        BinaryOp::Gt => compare(&l, &r).map(|o| EvalValue::Bool(o == std::cmp::Ordering::Greater)),
        BinaryOp::Gte => compare(&l, &r).map(|o| {
            EvalValue::Bool(o == std::cmp::Ordering::Greater || o == std::cmp::Ordering::Equal)
        }),
        BinaryOp::Add => arith_add(&l, &r),
        BinaryOp::Sub => arith(&l, &r, |a, b| a - b, |a, b| a - b, "-"),
        BinaryOp::Mul => arith(&l, &r, |a, b| a * b, |a, b| a * b, "*"),
        BinaryOp::Div => match (&l, &r) {
            (EvalValue::Integer(_), EvalValue::Integer(0))
            | (EvalValue::Double(_), EvalValue::Double(0.0)) => Ok(EvalValue::Null),
            _ => arith(&l, &r, |a, b| a / b, |a, b| a / b, "/"),
        },
    }
}

fn arith_add(l: &EvalValue, r: &EvalValue) -> Result<EvalValue, EvalError> {
    if let (EvalValue::String(a), EvalValue::String(b)) = (l, r) {
        return Ok(EvalValue::String(format!("{a}{b}")));
    }
    arith(l, r, |a, b| a + b, |a, b| a + b, "+")
}

fn arith(
    l: &EvalValue,
    r: &EvalValue,
    fi: fn(i64, i64) -> i64,
    fd: fn(f64, f64) -> f64,
    op: &str,
) -> Result<EvalValue, EvalError> {
    match (l, r) {
        (EvalValue::Integer(a), EvalValue::Integer(b)) => Ok(EvalValue::Integer(fi(*a, *b))),
        (EvalValue::Integer(a), EvalValue::Double(b)) => Ok(EvalValue::Double(fd(*a as f64, *b))),
        (EvalValue::Double(a), EvalValue::Integer(b)) => Ok(EvalValue::Double(fd(*a, *b as f64))),
        (EvalValue::Double(a), EvalValue::Double(b)) => Ok(EvalValue::Double(fd(*a, *b))),
        _ => Err(type_mismatch(op, l, r)),
    }
}

fn compare(l: &EvalValue, r: &EvalValue) -> Result<std::cmp::Ordering, EvalError> {
    use std::cmp::Ordering;
    match (l, r) {
        (EvalValue::Integer(a), EvalValue::Integer(b)) => Ok(a.cmp(b)),
        (EvalValue::Integer(a), EvalValue::Double(b)) => f64_cmp(*a as f64, *b),
        (EvalValue::Double(a), EvalValue::Integer(b)) => f64_cmp(*a, *b as f64),
        (EvalValue::Double(a), EvalValue::Double(b)) => f64_cmp(*a, *b),
        (EvalValue::String(a), EvalValue::String(b)) => Ok(a.cmp(b)),
        (EvalValue::Bool(a), EvalValue::Bool(b)) => Ok(a.cmp(b)),
        (EvalValue::Null, _) | (_, EvalValue::Null) => Ok(Ordering::Equal),
        _ => Err(type_mismatch("compare", l, r)),
    }
}

fn f64_cmp(a: f64, b: f64) -> Result<std::cmp::Ordering, EvalError> {
    a.partial_cmp(&b)
        .ok_or_else(|| EvalError::TypeMismatch("NaN comparison".into()))
}

fn value_eq(l: &EvalValue, r: &EvalValue) -> bool {
    match (l, r) {
        (EvalValue::Integer(a), EvalValue::Double(b)) => (*a as f64 - *b).abs() < f64::EPSILON,
        (EvalValue::Double(a), EvalValue::Integer(b)) => (*a - *b as f64).abs() < f64::EPSILON,
        _ => l == r,
    }
}

fn type_mismatch(op: &str, l: &EvalValue, r: &EvalValue) -> EvalError {
    EvalError::TypeMismatch(format!(
        "operator '{op}' not defined for {:?} and {:?}",
        l.type_hint(),
        r.type_hint()
    ))
}

fn eval_call(name: &str, args: &[Expr], row: &Row) -> Result<EvalValue, EvalError> {
    let name_lc = name.to_ascii_lowercase();
    match name_lc.as_str() {
        "is_null" => {
            need_arity(name, args, 1)?;
            Ok(EvalValue::Bool(matches!(eval(&args[0], row)?, EvalValue::Null)))
        }
        "is_not_null" => {
            need_arity(name, args, 1)?;
            Ok(EvalValue::Bool(!matches!(
                eval(&args[0], row)?,
                EvalValue::Null
            )))
        }
        "title_case" => {
            need_arity(name, args, 1)?;
            let v = eval(&args[0], row)?;
            match v {
                EvalValue::String(s) => Ok(EvalValue::String(to_title_case(&s))),
                EvalValue::Null => Ok(EvalValue::Null),
                other => Err(EvalError::TypeMismatch(format!(
                    "title_case expects String, got {:?}",
                    other.type_hint()
                ))),
            }
        }
        "lower" => unary_string(name, args, row, |s| s.to_lowercase()),
        "upper" => unary_string(name, args, row, |s| s.to_uppercase()),
        "trim" => unary_string(name, args, row, |s| s.trim().to_string()),
        "clean_string" => unary_string(name, args, row, |s| {
            s.split_whitespace().collect::<Vec<_>>().join(" ")
        }),
        "concat" => {
            need_arity(name, args, 2)?;
            let a = eval(&args[0], row)?;
            let b = eval(&args[1], row)?;
            match (a, b) {
                (EvalValue::String(x), EvalValue::String(y)) => Ok(EvalValue::String(format!("{x}{y}"))),
                _ => Err(EvalError::TypeMismatch("concat expects two strings".into())),
            }
        }
        "abs" => {
            need_arity(name, args, 1)?;
            match eval(&args[0], row)? {
                EvalValue::Integer(i) => Ok(EvalValue::Integer(i.abs())),
                EvalValue::Double(d) => Ok(EvalValue::Double(d.abs())),
                other => Err(EvalError::TypeMismatch(format!(
                    "abs expects numeric, got {:?}",
                    other.type_hint()
                ))),
            }
        }
        "cast" => {
            need_arity(name, args, 2)?;
            let v = eval(&args[0], row)?;
            let target = match &args[1] {
                Expr::Lit(Literal::String(s)) => s.clone(),
                _ => {
                    return Err(EvalError::TypeMismatch(
                        "cast target must be a String literal".into(),
                    ));
                }
            };
            cast_value(v, &target)
        }
        _ => Err(EvalError::UnknownFunction(name.to_string())),
    }
}

fn need_arity(name: &str, args: &[Expr], expected: usize) -> Result<(), EvalError> {
    if args.len() != expected {
        return Err(EvalError::Arity {
            name: name.to_string(),
            expected,
            got: args.len(),
        });
    }
    Ok(())
}

fn unary_string(
    name: &str,
    args: &[Expr],
    row: &Row,
    f: impl Fn(&str) -> String,
) -> Result<EvalValue, EvalError> {
    need_arity(name, args, 1)?;
    match eval(&args[0], row)? {
        EvalValue::String(s) => Ok(EvalValue::String(f(&s))),
        EvalValue::Null => Ok(EvalValue::Null),
        other => Err(EvalError::TypeMismatch(format!(
            "{name} expects String, got {:?}",
            other.type_hint()
        ))),
    }
}

fn cast_value(value: EvalValue, target: &str) -> Result<EvalValue, EvalError> {
    let upper = target.trim().to_ascii_uppercase();
    match (upper.as_str(), &value) {
        ("STRING", _) => Ok(EvalValue::String(match value {
            EvalValue::String(s) => s,
            EvalValue::Integer(i) => i.to_string(),
            EvalValue::Double(d) => d.to_string(),
            EvalValue::Bool(b) => b.to_string(),
            EvalValue::Null => "".to_string(),
        })),
        ("INTEGER", EvalValue::Integer(_)) => Ok(value),
        ("INTEGER", EvalValue::Double(d)) => Ok(EvalValue::Integer(*d as i64)),
        ("INTEGER", EvalValue::String(s)) => s
            .parse::<i64>()
            .map(EvalValue::Integer)
            .map_err(|_| EvalError::TypeMismatch(format!("cannot cast '{s}' to INTEGER"))),
        ("LONG", EvalValue::Integer(i)) => Ok(EvalValue::Integer(*i)),
        ("LONG", EvalValue::Double(d)) => Ok(EvalValue::Integer(*d as i64)),
        ("LONG", EvalValue::String(s)) => s
            .parse::<i64>()
            .map(EvalValue::Integer)
            .map_err(|_| EvalError::TypeMismatch(format!("cannot cast '{s}' to LONG"))),
        ("DOUBLE" | "DECIMAL", EvalValue::Integer(i)) => Ok(EvalValue::Double(*i as f64)),
        ("DOUBLE" | "DECIMAL", EvalValue::Double(_)) => Ok(value),
        ("DOUBLE" | "DECIMAL", EvalValue::String(s)) => s
            .parse::<f64>()
            .map(EvalValue::Double)
            .map_err(|_| EvalError::TypeMismatch(format!("cannot cast '{s}' to DOUBLE"))),
        ("BOOLEAN", EvalValue::Bool(_)) => Ok(value),
        ("BOOLEAN", EvalValue::String(s)) => match s.to_ascii_lowercase().as_str() {
            "true" => Ok(EvalValue::Bool(true)),
            "false" => Ok(EvalValue::Bool(false)),
            _ => Err(EvalError::TypeMismatch(format!("cannot cast '{s}' to BOOLEAN"))),
        },
        (_, EvalValue::Null) => Ok(EvalValue::Null),
        (target, value) => Err(EvalError::TypeMismatch(format!(
            "cannot cast {:?} to {target}",
            value.type_hint()
        ))),
    }
}

fn to_title_case(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    let mut prev_alpha = false;
    for ch in s.chars() {
        if ch.is_alphabetic() {
            if prev_alpha {
                out.extend(ch.to_lowercase());
            } else {
                out.extend(ch.to_uppercase());
            }
            prev_alpha = true;
        } else {
            out.push(ch);
            prev_alpha = false;
        }
    }
    out
}
