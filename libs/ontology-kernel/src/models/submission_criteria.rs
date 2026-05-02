//! TASK C — Submission criteria AST.
//!
//! Authored representation of the per-action gate documented in
//! `docs_original_palantir_foundry/foundry-docs/Ontology building/Define
//! Ontologies/Action types/Submission criteria.md`.
//!
//! The AST is persisted as JSONB on `action_types.submission_criteria`
//! (migration `20260502000000_add_submission_criteria.sql`). The
//! evaluator lives in [`crate::domain::submission_eval`].
//!
//! Each node owns its own optional `failure_message`; per the Foundry
//! contract, when a node fails the evaluator surfaces ITS message and
//! suppresses the messages of failing descendants. Leaves without an
//! explicit message contribute a synthesized "criterion failed" string so
//! the user-facing list is never empty.

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// Operand resolution kinds available in the authoring UI. Mirrors the
/// Foundry "Current User" / "Parameter" / "Property of parameter" / "Static"
/// templates.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum Operand {
    /// Reference to an action input field by name. Resolves to the
    /// already-coerced parameter value.
    Param { name: String },
    /// Property dereference of a parameter value: `param.property`. The
    /// referenced parameter must be an object map (e.g. an object reference
    /// expanded via `materialize_parameters`).
    ParamProperty { param: String, property: String },
    /// Attribute of the calling user, sourced from the JWT claims.
    CurrentUser { attribute: UserAttr },
    /// Static value authored at design time. Always JSON.
    Static { value: Value },
}

/// Subset of `auth_middleware::Claims` exposed to submission criteria.
/// Anything outside this set must NOT be addressable from authored criteria
/// to keep the evaluation surface auditable and deterministic.
#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum UserAttr {
    UserId,
    Email,
    OrganizationId,
    Roles,
    Permissions,
    AuthMethods,
}

/// Operators supported by the evaluator. The vocabulary matches
/// `Action types/Submission criteria.md` exactly (14 ops).
#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum Operator {
    Is,
    IsNot,
    Matches,
    Lt,
    Lte,
    Gt,
    Gte,
    Includes,
    IncludesAny,
    IsIncludedIn,
    EachIs,
    EachIsNot,
    IsEmpty,
    IsNotEmpty,
}

/// Tree of submission criteria evaluated in
/// [`crate::domain::submission_eval::evaluate`].
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum SubmissionNode {
    Leaf {
        left: Operand,
        op: Operator,
        right: Operand,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        failure_message: Option<String>,
    },
    All {
        children: Vec<SubmissionNode>,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        failure_message: Option<String>,
    },
    Any {
        children: Vec<SubmissionNode>,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        failure_message: Option<String>,
    },
    Not {
        child: Box<SubmissionNode>,
        #[serde(default, skip_serializing_if = "Option::is_none")]
        failure_message: Option<String>,
    },
}

impl SubmissionNode {
    /// Convenience constructor used by tests / programmatic seeding.
    pub fn leaf(left: Operand, op: Operator, right: Operand) -> Self {
        Self::Leaf {
            left,
            op,
            right,
            failure_message: None,
        }
    }

    /// Returns the failure message attached to this node, if any.
    pub fn failure_message(&self) -> Option<&str> {
        match self {
            Self::Leaf {
                failure_message, ..
            }
            | Self::All {
                failure_message, ..
            }
            | Self::Any {
                failure_message, ..
            }
            | Self::Not {
                failure_message, ..
            } => failure_message.as_deref(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn round_trip_serde_preserves_shape() {
        let node = SubmissionNode::All {
            failure_message: Some("policy".to_string()),
            children: vec![
                SubmissionNode::Leaf {
                    left: Operand::CurrentUser {
                        attribute: UserAttr::Roles,
                    },
                    op: Operator::Includes,
                    right: Operand::Static {
                        value: json!("ontology.editor"),
                    },
                    failure_message: Some("must be editor".to_string()),
                },
                SubmissionNode::Not {
                    child: Box::new(SubmissionNode::Leaf {
                        left: Operand::Param {
                            name: "amount".to_string(),
                        },
                        op: Operator::Gt,
                        right: Operand::Static {
                            value: json!(1000),
                        },
                        failure_message: None,
                    }),
                    failure_message: None,
                },
            ],
        };

        let raw = serde_json::to_value(&node).expect("serialize");
        let back: SubmissionNode = serde_json::from_value(raw).expect("deserialize");
        assert_eq!(node, back);
    }
}
