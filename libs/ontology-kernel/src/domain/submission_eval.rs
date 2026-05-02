//! TASK C — Submission criteria evaluator.
//!
//! Pure-Rust, side-effect-free evaluation of the AST defined in
//! [`crate::models::submission_criteria`]. Called by
//! `handlers::actions::{validate_action, execute_loaded_action}` after
//! `ensure_action_actor_permission` and before `plan_action` builds the
//! mutation plan.
//!
//! Failure-message semantics (per
//! `docs_original_palantir_foundry/foundry-docs/Ontology building/Define
//! Ontologies/Action types/Submission criteria.md`):
//!
//! * When a node fails AND owns a `failure_message`, that message is
//!   surfaced and the messages of failing descendants are suppressed.
//! * When a failing node has no message, the messages of its failing
//!   children are surfaced instead.
//! * Leaves with no message synthesize a deterministic "criterion failed"
//!   string so the user-facing list is never empty.

use std::collections::HashMap;

use auth_middleware::Claims;
use regex::Regex;
use serde_json::Value;

use crate::models::submission_criteria::{Operand, Operator, SubmissionNode, UserAttr};

/// Inputs available to the evaluator. Built once per request from the
/// authored action, the materialized parameters and the calling JWT.
#[derive(Debug, Clone)]
pub struct EvaluationContext<'a> {
    pub parameters: &'a HashMap<String, Value>,
    pub claims: &'a Claims,
}

/// Evaluate the AST rooted at `node`. Returns `Ok(())` when every branch
/// is satisfied, or `Err(messages)` with the user-facing failure messages
/// (deduplicated, original order preserved).
pub fn evaluate(node: &SubmissionNode, ctx: &EvaluationContext<'_>) -> Result<(), Vec<String>> {
    let mut out = Vec::new();
    if !evaluate_node(node, ctx, &mut out) {
        if out.is_empty() {
            out.push("submission criteria failed".to_string());
        }
        // Deduplicate while preserving order.
        let mut seen = std::collections::HashSet::new();
        out.retain(|msg| seen.insert(msg.clone()));
        return Err(out);
    }
    Ok(())
}

/// Evaluate a single subtree. Returns true on success. Appends failure
/// messages produced by THIS subtree to `out`. Callers decide whether to
/// keep them when a parent owns its own message.
fn evaluate_node(
    node: &SubmissionNode,
    ctx: &EvaluationContext<'_>,
    out: &mut Vec<String>,
) -> bool {
    match node {
        SubmissionNode::Leaf {
            left,
            op,
            right,
            failure_message,
        } => {
            let lhs = resolve(left, ctx);
            let rhs = resolve(right, ctx);
            let ok = apply_operator(*op, lhs.as_ref(), rhs.as_ref());
            if !ok {
                out.push(
                    failure_message
                        .clone()
                        .unwrap_or_else(|| synthesize_leaf_message(left, *op, right)),
                );
            }
            ok
        }
        SubmissionNode::All {
            children,
            failure_message,
        } => {
            let mut child_msgs = Vec::new();
            let mut all_ok = true;
            for child in children {
                if !evaluate_node(child, ctx, &mut child_msgs) {
                    all_ok = false;
                }
            }
            if all_ok {
                true
            } else {
                push_with_override(failure_message.as_deref(), child_msgs, out);
                false
            }
        }
        SubmissionNode::Any {
            children,
            failure_message,
        } => {
            // Empty `any` is conventionally true (no constraint).
            if children.is_empty() {
                return true;
            }
            let mut child_msgs = Vec::new();
            for child in children {
                let mut local = Vec::new();
                if evaluate_node(child, ctx, &mut local) {
                    return true;
                }
                child_msgs.extend(local);
            }
            push_with_override(failure_message.as_deref(), child_msgs, out);
            false
        }
        SubmissionNode::Not {
            child,
            failure_message,
        } => {
            let mut sink = Vec::new();
            let inner = evaluate_node(child, ctx, &mut sink);
            // NOT inverts truthiness; we discard the inner's failure
            // messages because the inner *succeeded* when we fail and
            // vice versa.
            if !inner {
                true
            } else {
                out.push(
                    failure_message
                        .clone()
                        .unwrap_or_else(|| "negated criterion was satisfied".to_string()),
                );
                false
            }
        }
    }
}

fn push_with_override(
    failure_message: Option<&str>,
    child_msgs: Vec<String>,
    out: &mut Vec<String>,
) {
    match failure_message {
        Some(msg) => out.push(msg.to_string()),
        None => out.extend(child_msgs),
    }
}

fn synthesize_leaf_message(left: &Operand, op: Operator, right: &Operand) -> String {
    format!(
        "criterion failed: {} {} {}",
        operand_label(left),
        operator_label(op),
        operand_label(right),
    )
}

fn operand_label(op: &Operand) -> String {
    match op {
        Operand::Param { name } => format!("parameter '{name}'"),
        Operand::ParamProperty { param, property } => {
            format!("parameter '{param}.{property}'")
        }
        Operand::CurrentUser { attribute } => format!("current user.{attribute:?}"),
        Operand::Static { value } => value.to_string(),
    }
}

fn operator_label(op: Operator) -> &'static str {
    match op {
        Operator::Is => "is",
        Operator::IsNot => "is not",
        Operator::Matches => "matches",
        Operator::Lt => "<",
        Operator::Lte => "<=",
        Operator::Gt => ">",
        Operator::Gte => ">=",
        Operator::Includes => "includes",
        Operator::IncludesAny => "includes any of",
        Operator::IsIncludedIn => "is included in",
        Operator::EachIs => "each is",
        Operator::EachIsNot => "each is not",
        Operator::IsEmpty => "is empty",
        Operator::IsNotEmpty => "is not empty",
    }
}

// ---------------------------------------------------------------------------
// Operand resolution
// ---------------------------------------------------------------------------

fn resolve(operand: &Operand, ctx: &EvaluationContext<'_>) -> Option<Value> {
    match operand {
        Operand::Param { name } => ctx.parameters.get(name).cloned(),
        Operand::ParamProperty { param, property } => ctx
            .parameters
            .get(param)
            .and_then(|val| val.as_object())
            .and_then(|obj| obj.get(property))
            .cloned(),
        Operand::CurrentUser { attribute } => Some(resolve_user_attr(*attribute, ctx.claims)),
        Operand::Static { value } => Some(value.clone()),
    }
}

fn resolve_user_attr(attribute: UserAttr, claims: &Claims) -> Value {
    match attribute {
        UserAttr::UserId => Value::String(claims.sub.to_string()),
        UserAttr::Email => Value::String(claims.email.clone()),
        UserAttr::OrganizationId => claims
            .org_id
            .map(|id| Value::String(id.to_string()))
            .unwrap_or(Value::Null),
        UserAttr::Roles => Value::Array(
            claims
                .roles
                .iter()
                .map(|r| Value::String(r.clone()))
                .collect(),
        ),
        UserAttr::Permissions => Value::Array(
            claims
                .permissions
                .iter()
                .map(|p| Value::String(p.clone()))
                .collect(),
        ),
        UserAttr::AuthMethods => Value::Array(
            claims
                .auth_methods
                .iter()
                .map(|m| Value::String(m.clone()))
                .collect(),
        ),
    }
}

// ---------------------------------------------------------------------------
// Operator application
// ---------------------------------------------------------------------------

fn apply_operator(op: Operator, lhs: Option<&Value>, rhs: Option<&Value>) -> bool {
    match op {
        Operator::IsEmpty => is_empty(lhs),
        Operator::IsNotEmpty => !is_empty(lhs),
        Operator::Is => json_eq(lhs, rhs),
        Operator::IsNot => !json_eq(lhs, rhs),
        Operator::Matches => match (lhs, rhs) {
            (Some(Value::String(haystack)), Some(Value::String(pattern))) => Regex::new(pattern)
                .map(|re| re.is_match(haystack))
                .unwrap_or(false),
            _ => false,
        },
        Operator::Lt => compare(lhs, rhs).map(|o| o.is_lt()).unwrap_or(false),
        Operator::Lte => compare(lhs, rhs).map(|o| o.is_le()).unwrap_or(false),
        Operator::Gt => compare(lhs, rhs).map(|o| o.is_gt()).unwrap_or(false),
        Operator::Gte => compare(lhs, rhs).map(|o| o.is_ge()).unwrap_or(false),
        Operator::Includes => match (lhs, rhs) {
            (Some(Value::Array(items)), Some(needle)) => items.iter().any(|v| v == needle),
            (Some(Value::String(haystack)), Some(Value::String(needle))) => {
                haystack.contains(needle.as_str())
            }
            _ => false,
        },
        Operator::IncludesAny => match (lhs, rhs) {
            (Some(Value::Array(items)), Some(Value::Array(needles))) => {
                needles.iter().any(|n| items.iter().any(|v| v == n))
            }
            _ => false,
        },
        Operator::IsIncludedIn => match (lhs, rhs) {
            (Some(needle), Some(Value::Array(items))) => items.iter().any(|v| v == needle),
            _ => false,
        },
        Operator::EachIs => match (lhs, rhs) {
            (Some(Value::Array(items)), Some(needle)) => items.iter().all(|v| v == needle),
            _ => false,
        },
        Operator::EachIsNot => match (lhs, rhs) {
            (Some(Value::Array(items)), Some(needle)) => items.iter().all(|v| v != needle),
            _ => false,
        },
    }
}

fn is_empty(v: Option<&Value>) -> bool {
    match v {
        None | Some(Value::Null) => true,
        Some(Value::String(s)) => s.is_empty(),
        Some(Value::Array(a)) => a.is_empty(),
        Some(Value::Object(o)) => o.is_empty(),
        _ => false,
    }
}

fn json_eq(lhs: Option<&Value>, rhs: Option<&Value>) -> bool {
    match (lhs, rhs) {
        (Some(a), Some(b)) => a == b,
        (None, None) => true,
        _ => false,
    }
}

fn compare(lhs: Option<&Value>, rhs: Option<&Value>) -> Option<std::cmp::Ordering> {
    match (lhs?, rhs?) {
        (Value::Number(a), Value::Number(b)) => a.as_f64()?.partial_cmp(&b.as_f64()?),
        (Value::String(a), Value::String(b)) => Some(a.cmp(b)),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use auth_middleware::claims::SessionScope;
    use chrono::Utc;
    use serde_json::json;
    use uuid::Uuid;

    fn claims_with_roles(roles: Vec<&str>) -> Claims {
        Claims {
            sub: Uuid::nil(),
            iat: Utc::now().timestamp(),
            exp: Utc::now().timestamp() + 60,
            iss: None,
            aud: None,
            jti: Uuid::nil(),
            email: "alice@example.com".to_string(),
            name: "Alice".to_string(),
            roles: roles.into_iter().map(String::from).collect(),
            permissions: vec![],
            org_id: None,
            attributes: Value::Null,
            auth_methods: vec![],
            token_use: None,
            api_key_id: None,
            session_kind: None,
            session_scope: Some(SessionScope::default()),
        }
    }

    fn ctx<'a>(
        params: &'a HashMap<String, Value>,
        claims: &'a Claims,
    ) -> EvaluationContext<'a> {
        EvaluationContext {
            parameters: params,
            claims,
        }
    }

    #[test]
    fn leaf_is_passes_when_param_matches_literal() {
        let mut p = HashMap::new();
        p.insert("status".into(), json!("approved"));
        let claims = claims_with_roles(vec![]);
        let node = SubmissionNode::leaf(
            Operand::Param {
                name: "status".into(),
            },
            Operator::Is,
            Operand::Static {
                value: json!("approved"),
            },
        );
        assert!(evaluate(&node, &ctx(&p, &claims)).is_ok());
    }

    #[test]
    fn leaf_failure_uses_authored_message() {
        let p = HashMap::new();
        let claims = claims_with_roles(vec!["viewer"]);
        let node = SubmissionNode::Leaf {
            left: Operand::CurrentUser {
                attribute: UserAttr::Roles,
            },
            op: Operator::Includes,
            right: Operand::Static {
                value: json!("ontology.editor"),
            },
            failure_message: Some("editor role required".to_string()),
        };
        let err = evaluate(&node, &ctx(&p, &claims)).unwrap_err();
        assert_eq!(err, vec!["editor role required"]);
    }

    #[test]
    fn all_uses_parent_message_and_suppresses_children() {
        let p = HashMap::new();
        let claims = claims_with_roles(vec![]);
        let node = SubmissionNode::All {
            failure_message: Some("policy violated".to_string()),
            children: vec![
                SubmissionNode::Leaf {
                    left: Operand::CurrentUser {
                        attribute: UserAttr::Roles,
                    },
                    op: Operator::Includes,
                    right: Operand::Static {
                        value: json!("admin"),
                    },
                    failure_message: Some("admin needed".into()),
                },
            ],
        };
        let err = evaluate(&node, &ctx(&p, &claims)).unwrap_err();
        assert_eq!(err, vec!["policy violated"]);
    }

    #[test]
    fn all_without_message_surfaces_child_messages() {
        let p = HashMap::new();
        let claims = claims_with_roles(vec![]);
        let node = SubmissionNode::All {
            failure_message: None,
            children: vec![
                SubmissionNode::Leaf {
                    left: Operand::CurrentUser {
                        attribute: UserAttr::Roles,
                    },
                    op: Operator::Includes,
                    right: Operand::Static {
                        value: json!("admin"),
                    },
                    failure_message: Some("need admin".into()),
                },
                SubmissionNode::Leaf {
                    left: Operand::CurrentUser {
                        attribute: UserAttr::Email,
                    },
                    op: Operator::IsNot,
                    right: Operand::Static {
                        value: json!("alice@example.com"),
                    },
                    failure_message: Some("not alice".into()),
                },
            ],
        };
        let err = evaluate(&node, &ctx(&p, &claims)).unwrap_err();
        assert_eq!(err, vec!["need admin", "not alice"]);
    }

    #[test]
    fn any_short_circuits_on_first_success() {
        let p = HashMap::new();
        let claims = claims_with_roles(vec!["viewer"]);
        let node = SubmissionNode::Any {
            failure_message: None,
            children: vec![
                SubmissionNode::Leaf {
                    left: Operand::CurrentUser {
                        attribute: UserAttr::Roles,
                    },
                    op: Operator::Includes,
                    right: Operand::Static {
                        value: json!("admin"),
                    },
                    failure_message: None,
                },
                SubmissionNode::Leaf {
                    left: Operand::CurrentUser {
                        attribute: UserAttr::Roles,
                    },
                    op: Operator::Includes,
                    right: Operand::Static {
                        value: json!("viewer"),
                    },
                    failure_message: None,
                },
            ],
        };
        assert!(evaluate(&node, &ctx(&p, &claims)).is_ok());
    }

    #[test]
    fn not_inverts_inner_truthiness() {
        let mut p = HashMap::new();
        p.insert("amount".into(), json!(50));
        let claims = claims_with_roles(vec![]);
        let node = SubmissionNode::Not {
            failure_message: Some("amount must be <= 100".to_string()),
            child: Box::new(SubmissionNode::Leaf {
                left: Operand::Param {
                    name: "amount".into(),
                },
                op: Operator::Gt,
                right: Operand::Static {
                    value: json!(100),
                },
                failure_message: None,
            }),
        };
        assert!(evaluate(&node, &ctx(&p, &claims)).is_ok());

        p.insert("amount".into(), json!(150));
        let err = evaluate(&node, &ctx(&p, &claims)).unwrap_err();
        assert_eq!(err, vec!["amount must be <= 100"]);
    }

    #[test]
    fn matches_evaluates_regex_against_string() {
        let mut p = HashMap::new();
        p.insert("email".into(), json!("alice@openfoundry.test"));
        let claims = claims_with_roles(vec![]);
        let node = SubmissionNode::leaf(
            Operand::Param {
                name: "email".into(),
            },
            Operator::Matches,
            Operand::Static {
                value: json!(r"^[a-z]+@openfoundry\.test$"),
            },
        );
        assert!(evaluate(&node, &ctx(&p, &claims)).is_ok());
    }

    #[test]
    fn each_is_not_passes_when_no_array_element_matches() {
        let mut p = HashMap::new();
        p.insert("tags".into(), json!(["green", "yellow"]));
        let claims = claims_with_roles(vec![]);
        let node = SubmissionNode::leaf(
            Operand::Param {
                name: "tags".into(),
            },
            Operator::EachIsNot,
            Operand::Static {
                value: json!("red"),
            },
        );
        assert!(evaluate(&node, &ctx(&p, &claims)).is_ok());
    }

    #[test]
    fn dedupes_repeated_messages() {
        let p = HashMap::new();
        let claims = claims_with_roles(vec![]);
        let same = SubmissionNode::Leaf {
            left: Operand::CurrentUser {
                attribute: UserAttr::Roles,
            },
            op: Operator::Includes,
            right: Operand::Static {
                value: json!("admin"),
            },
            failure_message: Some("admin needed".into()),
        };
        let node = SubmissionNode::All {
            failure_message: None,
            children: vec![same.clone(), same],
        };
        let err = evaluate(&node, &ctx(&p, &claims)).unwrap_err();
        assert_eq!(err, vec!["admin needed"]);
    }
}
