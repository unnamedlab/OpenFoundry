//! Typed signatures for the canonical Pipeline Builder transforms +
//! the scalar helpers visible inside expression bodies.
//!
//! Two surfaces:
//!
//! * [`scalar_signature`] — looks up a scalar function by name (used
//!   inside [`crate::infer`] when the expression contains a call). It
//!   returns the arity, the argument types it accepts, and the result
//!   type.
//! * [`transform_signature`] — looks up the nine canonical pipeline
//!   transforms documented in
//!   `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Workflows/Building pipelines/Getting started/Create a dataset batch pipeline with Pipeline Builder.md`
//!   plus the streaming/incremental docs (`window`, `pivot`).

use crate::types::PipelineType;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ScalarSignature {
    pub name: &'static str,
    pub params: Vec<ParamConstraint>,
    pub result: ResultRule,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ParamConstraint {
    /// Argument must be exactly this type.
    Exactly(PipelineType),
    /// Argument must be promotable to this type.
    Promotable(PipelineType),
    /// Argument must satisfy a family predicate.
    Numeric,
    Textual,
    Temporal,
    /// Anything goes — used for `cast` and runtime-only checks.
    Any,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ResultRule {
    Fixed(PipelineType),
    /// Promote to the LUB of the listed argument indexes.
    PromoteOf(Vec<usize>),
    /// Result is the type literal extracted from arg N (used for
    /// `cast(value, type_literal)`).
    TypeFromStringArg(usize),
}

pub fn scalar_signature(name: &str) -> Option<ScalarSignature> {
    let n = name.to_ascii_lowercase();
    Some(match n.as_str() {
        // String helpers.
        "title_case" | "lower" | "upper" | "trim" | "clean_string" => ScalarSignature {
            name: leak(&n),
            params: vec![ParamConstraint::Textual],
            result: ResultRule::Fixed(PipelineType::String),
        },
        "concat" => ScalarSignature {
            name: leak(&n),
            params: vec![ParamConstraint::Textual, ParamConstraint::Textual],
            result: ResultRule::Fixed(PipelineType::String),
        },
        // Numeric helpers.
        "abs" | "floor" | "ceil" | "round" => ScalarSignature {
            name: leak(&n),
            params: vec![ParamConstraint::Numeric],
            result: ResultRule::PromoteOf(vec![0]),
        },
        // Temporal.
        "to_date" => ScalarSignature {
            name: leak(&n),
            params: vec![ParamConstraint::Textual],
            result: ResultRule::Fixed(PipelineType::Date),
        },
        "to_timestamp" => ScalarSignature {
            name: leak(&n),
            params: vec![ParamConstraint::Textual],
            result: ResultRule::Fixed(PipelineType::Timestamp),
        },
        // Type cast: special-cased — second arg must be a STRING literal
        // naming the target type.
        "cast" => ScalarSignature {
            name: leak(&n),
            params: vec![ParamConstraint::Any, ParamConstraint::Exactly(PipelineType::String)],
            result: ResultRule::TypeFromStringArg(1),
        },
        // Boolean.
        "is_null" | "is_not_null" => ScalarSignature {
            name: leak(&n),
            params: vec![ParamConstraint::Any],
            result: ResultRule::Fixed(PipelineType::Boolean),
        },
        // Geometry helpers.
        "geom_within" => ScalarSignature {
            name: leak(&n),
            params: vec![
                ParamConstraint::Exactly(PipelineType::Geometry),
                ParamConstraint::Exactly(PipelineType::Geometry),
            ],
            result: ResultRule::Fixed(PipelineType::Boolean),
        },
        _ => return None,
    })
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TransformSignature {
    pub name: &'static str,
    /// The transform either takes one input dataset or two (join/union).
    pub min_inputs: usize,
    pub max_inputs: usize,
    /// Required keys on the node `config` JSON object (informative).
    pub required_config_keys: &'static [&'static str],
}

pub fn transform_signature(name: &str) -> Option<TransformSignature> {
    let n = name.to_ascii_lowercase();
    Some(match n.as_str() {
        "cast" => TransformSignature {
            name: "cast",
            min_inputs: 1,
            max_inputs: 1,
            required_config_keys: &["columns"],
        },
        "title_case" => TransformSignature {
            name: "title_case",
            min_inputs: 1,
            max_inputs: 1,
            required_config_keys: &["columns"],
        },
        "clean_string" => TransformSignature {
            name: "clean_string",
            min_inputs: 1,
            max_inputs: 1,
            required_config_keys: &["columns"],
        },
        "filter" => TransformSignature {
            name: "filter",
            min_inputs: 1,
            max_inputs: 1,
            required_config_keys: &["predicate"],
        },
        "join" => TransformSignature {
            name: "join",
            min_inputs: 2,
            max_inputs: 2,
            required_config_keys: &["how", "on"],
        },
        "union" => TransformSignature {
            name: "union",
            min_inputs: 2,
            max_inputs: usize::MAX,
            required_config_keys: &[],
        },
        "group_by" => TransformSignature {
            name: "group_by",
            min_inputs: 1,
            max_inputs: 1,
            required_config_keys: &["keys", "aggregations"],
        },
        "window" => TransformSignature {
            name: "window",
            min_inputs: 1,
            max_inputs: 1,
            required_config_keys: &["partition_by", "order_by"],
        },
        "pivot" => TransformSignature {
            name: "pivot",
            min_inputs: 1,
            max_inputs: 1,
            required_config_keys: &["pivot_column", "value_column"],
        },
        _ => return None,
    })
}

/// Parse a string like `"INTEGER"` / `"ARRAY<INTEGER>"` into a
/// [`PipelineType`]. Used by `cast(value, "INTEGER")`. Returns `None`
/// when the literal isn't a recognised type name.
pub fn parse_type_literal(literal: &str) -> Option<PipelineType> {
    let trimmed = literal.trim();
    let upper = trimmed.to_ascii_uppercase();
    match upper.as_str() {
        "BOOLEAN" => Some(PipelineType::Boolean),
        "INTEGER" => Some(PipelineType::Integer),
        "LONG" => Some(PipelineType::Long),
        "DOUBLE" => Some(PipelineType::Double),
        "DECIMAL" => Some(PipelineType::Decimal),
        "STRING" => Some(PipelineType::String),
        "DATE" => Some(PipelineType::Date),
        "TIMESTAMP" => Some(PipelineType::Timestamp),
        "GEOMETRY" => Some(PipelineType::Geometry),
        _ => {
            if let Some(rest) = upper.strip_prefix("ARRAY<").and_then(|s| s.strip_suffix('>')) {
                return parse_type_literal(rest).map(PipelineType::array_of);
            }
            None
        }
    }
}

/// Static-string interner so `ScalarSignature::name` stays `&'static`.
fn leak(s: &str) -> &'static str {
    Box::leak(s.to_owned().into_boxed_str())
}
