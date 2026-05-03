//! Pipeline parameter model.
//!
//! Per the Foundry doc § "Parameterized pipelines", a JobSpec's
//! `logic_payload` may declare a list of [`Param`]s that the build
//! pass injects into JobExecutionContext (kwargs in Python, variables
//! in SQL). Parameters carry a name, a typed default, and a
//! required-ness flag — exactly the shape the Foundry "Schedule
//! editor" exposes when picking a parameter value to override.

use serde::{Deserialize, Serialize};
use serde_json::Value;
use thiserror::Error;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Deserialize, Serialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum ParamType {
    String,
    Integer,
    Float,
    Boolean,
}

impl ParamType {
    pub fn as_str(&self) -> &'static str {
        match self {
            ParamType::String => "STRING",
            ParamType::Integer => "INTEGER",
            ParamType::Float => "FLOAT",
            ParamType::Boolean => "BOOLEAN",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Deserialize, Serialize)]
pub struct Param {
    pub name: String,
    #[serde(rename = "type")]
    pub param_type: ParamType,
    #[serde(default)]
    pub default_value: Option<Value>,
    #[serde(default)]
    pub required: bool,
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum ParamValidationError {
    #[error("parameter '{0}' is required but no value was supplied")]
    MissingRequired(String),
    #[error("parameter '{0}' got value of wrong type (expected {expected}, got {got})", expected = .1, got = .2)]
    WrongType(String, &'static str, &'static str),
    #[error("parameter '{0}' is not declared on this pipeline")]
    Unknown(String),
}

/// Validate `overrides` against `declared`. Each override must match
/// the declared param's type; unknown params are rejected. Defaults
/// are NOT auto-filled here — callers compose [`merge_with_defaults`]
/// when they need the runtime kwargs map.
pub fn validate_overrides(
    declared: &[Param],
    overrides: &serde_json::Map<String, Value>,
) -> Result<(), ParamValidationError> {
    for (name, value) in overrides {
        let param = declared
            .iter()
            .find(|p| p.name == *name)
            .ok_or_else(|| ParamValidationError::Unknown(name.clone()))?;
        if !value_matches_type(value, param.param_type) {
            return Err(ParamValidationError::WrongType(
                name.clone(),
                param.param_type.as_str(),
                value_kind(value),
            ));
        }
    }
    for param in declared.iter().filter(|p| p.required) {
        if !overrides.contains_key(&param.name) && param.default_value.is_none() {
            return Err(ParamValidationError::MissingRequired(param.name.clone()));
        }
    }
    Ok(())
}

/// Compose the runtime kwargs map: per-param default first, override
/// (when present) wins. Required params with no override and no
/// default surface as a [`ParamValidationError::MissingRequired`].
pub fn merge_with_defaults(
    declared: &[Param],
    overrides: &serde_json::Map<String, Value>,
) -> Result<serde_json::Map<String, Value>, ParamValidationError> {
    let mut out = serde_json::Map::with_capacity(declared.len());
    for param in declared {
        let value = overrides
            .get(&param.name)
            .cloned()
            .or_else(|| param.default_value.clone());
        match value {
            Some(v) => {
                if !value_matches_type(&v, param.param_type) {
                    return Err(ParamValidationError::WrongType(
                        param.name.clone(),
                        param.param_type.as_str(),
                        value_kind(&v),
                    ));
                }
                out.insert(param.name.clone(), v);
            }
            None if param.required => {
                return Err(ParamValidationError::MissingRequired(param.name.clone()));
            }
            None => {}
        }
    }
    Ok(out)
}

fn value_matches_type(value: &Value, param_type: ParamType) -> bool {
    match (value, param_type) {
        (Value::String(_), ParamType::String) => true,
        (Value::Number(n), ParamType::Integer) => n.is_i64() || n.is_u64(),
        (Value::Number(_), ParamType::Float) => true,
        (Value::Bool(_), ParamType::Boolean) => true,
        (Value::Null, _) => false,
        _ => false,
    }
}

fn value_kind(value: &Value) -> &'static str {
    match value {
        Value::Null => "null",
        Value::Bool(_) => "BOOLEAN",
        Value::Number(_) => "NUMBER",
        Value::String(_) => "STRING",
        Value::Array(_) => "ARRAY",
        Value::Object(_) => "OBJECT",
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn declared() -> Vec<Param> {
        vec![
            Param {
                name: "deployment_key".into(),
                param_type: ParamType::String,
                default_value: None,
                required: true,
            },
            Param {
                name: "limit".into(),
                param_type: ParamType::Integer,
                default_value: Some(json!(1000)),
                required: false,
            },
            Param {
                name: "ratio".into(),
                param_type: ParamType::Float,
                default_value: Some(json!(0.5)),
                required: false,
            },
        ]
    }

    #[test]
    fn validate_accepts_required_override() {
        let mut overrides = serde_json::Map::new();
        overrides.insert("deployment_key".into(), json!("eu-west"));
        assert!(validate_overrides(&declared(), &overrides).is_ok());
    }

    #[test]
    fn validate_rejects_missing_required() {
        let overrides = serde_json::Map::new();
        let err = validate_overrides(&declared(), &overrides).unwrap_err();
        assert!(matches!(err, ParamValidationError::MissingRequired(name) if name == "deployment_key"));
    }

    #[test]
    fn validate_rejects_unknown_param() {
        let mut overrides = serde_json::Map::new();
        overrides.insert("ghost".into(), json!("x"));
        let err = validate_overrides(&declared(), &overrides).unwrap_err();
        assert!(matches!(err, ParamValidationError::Unknown(_)));
    }

    #[test]
    fn validate_rejects_wrong_type() {
        let mut overrides = serde_json::Map::new();
        overrides.insert("deployment_key".into(), json!(42));
        let err = validate_overrides(&declared(), &overrides).unwrap_err();
        assert!(matches!(err, ParamValidationError::WrongType(_, _, _)));
    }

    #[test]
    fn merge_fills_defaults_when_override_absent() {
        let mut overrides = serde_json::Map::new();
        overrides.insert("deployment_key".into(), json!("eu-west"));
        let merged = merge_with_defaults(&declared(), &overrides).unwrap();
        assert_eq!(merged.get("deployment_key"), Some(&json!("eu-west")));
        assert_eq!(merged.get("limit"), Some(&json!(1000)));
        assert_eq!(merged.get("ratio"), Some(&json!(0.5)));
    }

    #[test]
    fn merge_overrides_default() {
        let mut overrides = serde_json::Map::new();
        overrides.insert("deployment_key".into(), json!("eu-west"));
        overrides.insert("limit".into(), json!(50));
        let merged = merge_with_defaults(&declared(), &overrides).unwrap();
        assert_eq!(merged.get("limit"), Some(&json!(50)));
    }
}
