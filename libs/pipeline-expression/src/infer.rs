//! Type inference for parsed Pipeline Builder expressions.
//!
//! Given a column environment (`name → PipelineType`) and an [`Expr`],
//! [`infer_expr`] returns the inferred type or a list of [`TypeError`]s
//! pinpointed to the offending column / call.
//!
//! Errors are non-fatal: when one branch of a binary expression doesn't
//! type-check we fall back to an `Unknown` placeholder so the rest of
//! the tree keeps validating. This is what makes the squiggle UI usable
//! on partially broken expressions.

use std::collections::HashMap;

use serde::{Deserialize, Serialize};
use thiserror::Error;

use crate::catalog::{
    ParamConstraint, ResultRule, parse_type_literal, scalar_signature,
};
use crate::parser::{BinaryOp, Expr, Literal, UnaryOp};
use crate::types::{PipelineType, can_promote, promote};

#[derive(Debug, Clone, Default)]
pub struct ColumnEnv {
    columns: HashMap<String, PipelineType>,
}

impl ColumnEnv {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn with(mut self, name: impl Into<String>, ty: PipelineType) -> Self {
        self.columns.insert(name.into(), ty);
        self
    }

    pub fn insert(&mut self, name: impl Into<String>, ty: PipelineType) {
        self.columns.insert(name.into(), ty);
    }

    pub fn lookup(&self, name: &str) -> Option<&PipelineType> {
        self.columns.get(name)
    }

    pub fn len(&self) -> usize {
        self.columns.len()
    }

    pub fn is_empty(&self) -> bool {
        self.columns.is_empty()
    }
}

#[derive(Debug, Clone, Error, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "kind", content = "detail")]
pub enum TypeError {
    #[error("unknown column '{0}'")]
    UnknownColumn(String),
    #[error("unknown function '{0}'")]
    UnknownFunction(String),
    #[error("function '{name}' expects {expected} args, got {got}")]
    Arity { name: String, expected: usize, got: usize },
    #[error("function '{name}' arg {index}: expected {expected}, got {got}")]
    ArgType {
        name: String,
        index: usize,
        expected: String,
        got: String,
    },
    #[error("operator '{op}' is not defined for {left} and {right}")]
    BinaryOp { op: String, left: String, right: String },
    #[error("operator '{op}' is not defined for {ty}")]
    UnaryOp { op: String, ty: String },
    #[error("cast target literal must be a known type, got '{0}'")]
    InvalidCastTarget(String),
}

/// Convenience alias for callers that want a fail-fast view: any error
/// short-circuits with `Err`. `infer_expr_collect` keeps going so the
/// UI can highlight every offending site at once.
pub fn infer_expr(expr: &Expr, env: &ColumnEnv) -> Result<PipelineType, Vec<TypeError>> {
    let mut errors = Vec::new();
    let inferred = infer_inner(expr, env, &mut errors);
    if errors.is_empty() {
        Ok(inferred.unwrap_or(PipelineType::String))
    } else {
        Err(errors)
    }
}

fn infer_inner(
    expr: &Expr,
    env: &ColumnEnv,
    errors: &mut Vec<TypeError>,
) -> Option<PipelineType> {
    match expr {
        Expr::Lit(lit) => Some(lit_type(lit)),
        Expr::Column(name) => match env.lookup(name) {
            Some(t) => Some(t.clone()),
            None => {
                errors.push(TypeError::UnknownColumn(name.clone()));
                None
            }
        },
        Expr::Unary { op, operand } => {
            let inner = infer_inner(operand, env, errors)?;
            match op {
                UnaryOp::Neg if inner.is_numeric() => Some(inner),
                UnaryOp::Neg => {
                    errors.push(TypeError::UnaryOp {
                        op: "-".into(),
                        ty: type_name(&inner),
                    });
                    None
                }
                UnaryOp::Not if matches!(inner, PipelineType::Boolean) => {
                    Some(PipelineType::Boolean)
                }
                UnaryOp::Not => {
                    errors.push(TypeError::UnaryOp {
                        op: "not".into(),
                        ty: type_name(&inner),
                    });
                    None
                }
            }
        }
        Expr::Binary { op, left, right } => {
            let l = infer_inner(left, env, errors);
            let r = infer_inner(right, env, errors);
            let (l, r) = match (l, r) {
                (Some(a), Some(b)) => (a, b),
                _ => return None,
            };
            type_binary(*op, &l, &r, errors)
        }
        Expr::Call { name, args } => infer_call(name, args, env, errors),
    }
}

fn type_binary(
    op: BinaryOp,
    l: &PipelineType,
    r: &PipelineType,
    errors: &mut Vec<TypeError>,
) -> Option<PipelineType> {
    let push_err = |errors: &mut Vec<TypeError>| {
        errors.push(TypeError::BinaryOp {
            op: format!("{op:?}"),
            left: type_name(l),
            right: type_name(r),
        });
    };

    if op.is_logical() {
        if matches!(l, PipelineType::Boolean) && matches!(r, PipelineType::Boolean) {
            return Some(PipelineType::Boolean);
        }
        push_err(errors);
        return None;
    }

    if op.is_comparison() {
        // Numeric vs numeric, temporal vs temporal, string vs string
        // are all OK. Cross-family is rejected.
        let ok = (l.is_numeric() && r.is_numeric())
            || (l.is_textual() && r.is_textual())
            || (l.is_temporal() && r.is_temporal())
            || l == r;
        if ok {
            return Some(PipelineType::Boolean);
        }
        push_err(errors);
        return None;
    }

    match op {
        BinaryOp::Add | BinaryOp::Sub | BinaryOp::Mul | BinaryOp::Div => {
            if l.is_numeric() && r.is_numeric() {
                return promote(l, r);
            }
            if matches!(op, BinaryOp::Add) && l.is_textual() && r.is_textual() {
                return Some(PipelineType::String);
            }
            push_err(errors);
            None
        }
        _ => unreachable!("comparison/logical handled above"),
    }
}

fn infer_call(
    name: &str,
    args: &[Expr],
    env: &ColumnEnv,
    errors: &mut Vec<TypeError>,
) -> Option<PipelineType> {
    let Some(sig) = scalar_signature(name) else {
        errors.push(TypeError::UnknownFunction(name.to_string()));
        return None;
    };

    if args.len() != sig.params.len() {
        errors.push(TypeError::Arity {
            name: name.to_string(),
            expected: sig.params.len(),
            got: args.len(),
        });
        return None;
    }

    let mut arg_types: Vec<Option<PipelineType>> = Vec::with_capacity(args.len());
    for (i, arg) in args.iter().enumerate() {
        let inferred = infer_inner(arg, env, errors);
        if let Some(actual) = &inferred {
            check_constraint(name, i, actual, &sig.params[i], errors);
        }
        arg_types.push(inferred);
    }

    match sig.result {
        ResultRule::Fixed(t) => Some(t),
        ResultRule::PromoteOf(idxs) => {
            let mut out: Option<PipelineType> = None;
            for &i in &idxs {
                if let Some(t) = arg_types.get(i).and_then(|x| x.as_ref()) {
                    out = match out {
                        None => Some(t.clone()),
                        Some(prev) => promote(&prev, t),
                    };
                }
            }
            out
        }
        ResultRule::TypeFromStringArg(i) => {
            let lit = match args.get(i) {
                Some(Expr::Lit(Literal::String(s))) => s.clone(),
                _ => {
                    errors.push(TypeError::ArgType {
                        name: name.to_string(),
                        index: i,
                        expected: "STRING literal".into(),
                        got: "expression".into(),
                    });
                    return None;
                }
            };
            match parse_type_literal(&lit) {
                Some(t) => Some(t),
                None => {
                    errors.push(TypeError::InvalidCastTarget(lit));
                    None
                }
            }
        }
    }
}

fn check_constraint(
    name: &str,
    index: usize,
    actual: &PipelineType,
    constraint: &ParamConstraint,
    errors: &mut Vec<TypeError>,
) {
    let push = |errors: &mut Vec<TypeError>, expected: &str| {
        errors.push(TypeError::ArgType {
            name: name.to_string(),
            index,
            expected: expected.to_string(),
            got: type_name(actual),
        });
    };
    match constraint {
        ParamConstraint::Any => {}
        ParamConstraint::Exactly(t) => {
            if actual != t {
                push(errors, &type_name(t));
            }
        }
        ParamConstraint::Promotable(t) => {
            if !can_promote(actual, t) {
                push(errors, &format!("promotable to {}", type_name(t)));
            }
        }
        ParamConstraint::Numeric => {
            if !actual.is_numeric() {
                push(errors, "numeric");
            }
        }
        ParamConstraint::Textual => {
            if !actual.is_textual() {
                push(errors, "string");
            }
        }
        ParamConstraint::Temporal => {
            if !actual.is_temporal() {
                push(errors, "date or timestamp");
            }
        }
    }
}

fn lit_type(lit: &Literal) -> PipelineType {
    match lit {
        Literal::Bool(_) => PipelineType::Boolean,
        Literal::Integer(_) => PipelineType::Integer,
        Literal::Double(_) => PipelineType::Double,
        Literal::String(_) => PipelineType::String,
        // Untyped null defaults to String — most pipeline columns are
        // string-shaped at the source, and a nullable string is the
        // safest default. Callers who care can `cast(null, "INTEGER")`.
        Literal::Null => PipelineType::String,
    }
}

fn type_name(t: &PipelineType) -> String {
    match t {
        PipelineType::Boolean => "Boolean".into(),
        PipelineType::Integer => "Integer".into(),
        PipelineType::Long => "Long".into(),
        PipelineType::Double => "Double".into(),
        PipelineType::Decimal => "Decimal".into(),
        PipelineType::String => "String".into(),
        PipelineType::Date => "Date".into(),
        PipelineType::Timestamp => "Timestamp".into(),
        PipelineType::Geometry => "Geometry".into(),
        PipelineType::Array { inner } => format!("Array<{}>", type_name(inner)),
        PipelineType::Struct { fields } => {
            let parts: Vec<String> = fields
                .iter()
                .map(|(n, t)| format!("{n}: {}", type_name(t)))
                .collect();
            format!("Struct<{}>", parts.join(", "))
        }
    }
}
