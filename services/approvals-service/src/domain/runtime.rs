use auth_middleware::{
    claims::Claims,
    jwt::{build_access_claims, encode_token},
};
use chrono::Utc;
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value, json};
use uuid::Uuid;

use crate::AppState;

#[derive(Debug, Clone, Serialize, Deserialize)]
struct StoredActionProposal {
    kind: String,
    action_id: Uuid,
    target_object_id: Option<Uuid>,
    parameters: Value,
    justification: Option<String>,
    summary: String,
    reasoning: Option<Value>,
    preview: Value,
    what_if_branch: Option<Value>,
    auto_apply_on_approval: bool,
    execution_identity: String,
}

#[derive(Debug, Clone)]
struct ResolvedActionInvocation {
    action_id: Uuid,
    target_object_id: Option<Uuid>,
    parameters: Value,
    justification: Option<String>,
    execution_identity: String,
}

pub fn insert_approval_decision(
    context: &mut Value,
    step_id: &str,
    decision: &str,
    decided_by: Uuid,
    payload: &Value,
    comment: Option<&str>,
) {
    ensure_object(context);
    let object = context.as_object_mut().expect("context must be object");
    let approvals = object
        .entry("approvals")
        .or_insert_with(|| Value::Object(Map::new()));

    ensure_object(approvals);
    approvals
        .as_object_mut()
        .expect("approvals must be object")
        .insert(
            step_id.to_string(),
            json!({
                "decision": decision,
                "decided_by": decided_by,
                "comment": comment,
                "payload": payload,
                "decided_at": Utc::now(),
            }),
        );

    object.insert(
        "last_approval_decision".to_string(),
        json!({
            "step_id": step_id,
            "decision": decision,
            "decided_by": decided_by,
            "comment": comment,
            "payload": payload,
        }),
    );
}

pub fn upsert_approval_review_payload(
    payload: &Value,
    decision: &str,
    decided_by: Uuid,
    comment: Option<&str>,
    decision_payload: &Value,
) -> Value {
    let mut next_payload = payload.clone();
    ensure_object(&mut next_payload);
    next_payload
        .as_object_mut()
        .expect("approval payload must be object")
        .insert(
            "decision_review".to_string(),
            json!({
                "decision": decision,
                "decided_by": decided_by,
                "comment": comment,
                "payload": decision_payload,
                "decided_at": Utc::now(),
            }),
        );
    next_payload
}

pub fn annotate_approval_proposal_execution(
    payload: &Value,
    status: &str,
    response: Option<&Value>,
    error: Option<&str>,
) -> Value {
    let mut next_payload = payload.clone();
    ensure_object(&mut next_payload);
    next_payload
        .as_object_mut()
        .expect("approval payload must be object")
        .insert(
            "proposal_execution".to_string(),
            json!({
                "status": status,
                "response": response,
                "error": error,
                "updated_at": Utc::now(),
            }),
        );
    next_payload
}

pub async fn apply_approval_proposal(
    state: &AppState,
    context: &mut Value,
    approval_step_id: &str,
    approval_payload: &Value,
    claims: &Claims,
) -> Result<Value, String> {
    let Some(proposal_value) = approval_payload.get("proposal") else {
        return Ok(Value::Null);
    };
    let proposal = serde_json::from_value::<StoredActionProposal>(proposal_value.clone())
        .map_err(|error| format!("invalid stored proposal payload: {error}"))?;

    if !proposal.auto_apply_on_approval {
        record_action_proposal(
            context,
            approval_step_id,
            &proposal,
            "approved_pending_manual_apply",
            None,
            None,
        );
        return Ok(Value::Null);
    }

    let invocation = ResolvedActionInvocation {
        action_id: proposal.action_id,
        target_object_id: proposal.target_object_id,
        parameters: proposal.parameters.clone(),
        justification: proposal.justification.clone(),
        execution_identity: proposal.execution_identity.clone(),
    };

    let response = match proposal.execution_identity.as_str() {
        "approver" => {
            let actor_token = issue_actor_token(state, claims)?;
            execute_ontology_action(state, &invocation, &actor_token).await
        }
        _ => {
            execute_ontology_action_as_service(state, &invocation, Some(claims.sub), claims.org_id)
                .await
        }
    };

    match response {
        Ok(response) => {
            record_action_proposal(
                context,
                approval_step_id,
                &proposal,
                "applied",
                Some(&response),
                None,
            );
            record_submitted_action(
                context,
                approval_step_id,
                &invocation,
                "applied",
                Some(&response),
                None,
            );
            Ok(response)
        }
        Err(error) => {
            record_action_proposal(
                context,
                approval_step_id,
                &proposal,
                "failed",
                None,
                Some(&error),
            );
            record_submitted_action(
                context,
                approval_step_id,
                &invocation,
                "failed",
                None,
                Some(&error),
            );
            Err(error)
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ApprovalContinuationAck {
    pub accepted: bool,
    pub approval_id: Uuid,
    #[serde(default)]
    pub response: Value,
}

pub async fn continue_workflow_after_approval(
    state: &AppState,
    approval: &crate::models::approval::WorkflowApproval,
    decision: &str,
    context: &Value,
) -> Result<ApprovalContinuationAck, String> {
    let endpoint = format!(
        "{}/internal/workflows/approvals/{}/continue",
        state.workflow_service_url.trim_end_matches('/'),
        approval.id,
    );

    let response = state
        .http_client
        .post(endpoint)
        .json(&json!({
            "workflow_id": approval.workflow_id,
            "workflow_run_id": approval.workflow_run_id,
            "step_id": approval.step_id,
            "decision": decision,
            "context": context,
        }))
        .send()
        .await
        .map_err(|error| format!("workflow continuation request failed: {error}"))?;

    let status = response.status();
    let raw_body = response
        .text()
        .await
        .map_err(|error| format!("failed to read workflow continuation body: {error}"))?;

    if status.is_success() {
        let response = if raw_body.trim().is_empty() {
            json!({"status": status.as_u16()})
        } else {
            serde_json::from_str::<Value>(&raw_body).unwrap_or_else(|_| json!({"raw": raw_body}))
        };
        Ok(ApprovalContinuationAck {
            accepted: true,
            approval_id: approval.id,
            response,
        })
    } else {
        let payload = if raw_body.trim().is_empty() {
            Value::Null
        } else {
            serde_json::from_str::<Value>(&raw_body).unwrap_or_else(|_| json!({ "raw": raw_body }))
        };
        let message = payload
            .get("error")
            .and_then(Value::as_str)
            .map(str::to_string)
            .or_else(|| payload.get("details").map(|details| details.to_string()))
            .unwrap_or_else(|| payload.to_string());
        Err(format!(
            "workflow continuation returned {}: {message}",
            status
        ))
    }
}

async fn execute_ontology_action_as_service(
    state: &AppState,
    invocation: &ResolvedActionInvocation,
    impersonated_actor_id: Option<Uuid>,
    org_id: Option<Uuid>,
) -> Result<Value, String> {
    let service_token = issue_service_token(state, impersonated_actor_id, org_id)?;
    execute_ontology_action(state, invocation, &service_token).await
}

async fn execute_ontology_action(
    state: &AppState,
    invocation: &ResolvedActionInvocation,
    bearer_token: &str,
) -> Result<Value, String> {
    post_ontology_json(
        state,
        bearer_token,
        &format!("/api/v1/ontology/actions/{}/execute", invocation.action_id),
        &json!({
            "target_object_id": invocation.target_object_id,
            "parameters": invocation.parameters,
            "justification": invocation.justification,
        }),
    )
    .await
}

async fn post_ontology_json(
    state: &AppState,
    bearer_token: &str,
    path: &str,
    body: &Value,
) -> Result<Value, String> {
    let endpoint = format!(
        "{}{}",
        state.ontology_service_url.trim_end_matches('/'),
        path
    );
    let response = state
        .http_client
        .post(endpoint)
        .header("authorization", bearer_token)
        .json(body)
        .send()
        .await
        .map_err(|error| format!("ontology request failed: {error}"))?;

    let status = response.status();
    let raw_body = response
        .text()
        .await
        .map_err(|error| format!("failed to read ontology response body: {error}"))?;
    let payload = if raw_body.trim().is_empty() {
        Value::Null
    } else {
        serde_json::from_str::<Value>(&raw_body).unwrap_or_else(|_| json!({ "raw": raw_body }))
    };

    if status.is_success() {
        Ok(payload)
    } else {
        let message = payload
            .get("error")
            .and_then(Value::as_str)
            .map(str::to_string)
            .or_else(|| payload.get("details").map(|details| details.to_string()))
            .unwrap_or_else(|| payload.to_string());
        Err(format!("ontology request returned {}: {message}", status))
    }
}

fn issue_service_token(
    state: &AppState,
    impersonated_actor_id: Option<Uuid>,
    org_id: Option<Uuid>,
) -> Result<String, String> {
    let mut attributes = Map::new();
    attributes.insert(
        "service".to_string(),
        Value::String("approvals-service".to_string()),
    );
    attributes.insert(
        "classification_clearance".to_string(),
        Value::String("pii".to_string()),
    );
    if let Some(actor_id) = impersonated_actor_id {
        attributes.insert(
            "impersonated_actor_id".to_string(),
            Value::String(actor_id.to_string()),
        );
    }

    let service_claims = build_access_claims(
        &state.jwt_config,
        Uuid::now_v7(),
        "approvals-service@internal.openfoundry",
        "approvals-service",
        vec!["admin".to_string()],
        vec!["*:*".to_string()],
        org_id,
        Value::Object(attributes),
        vec!["service".to_string()],
    );
    let token = encode_token(&state.jwt_config, &service_claims)
        .map_err(|error| format!("failed to issue approvals service token: {error}"))?;
    Ok(format!("Bearer {token}"))
}

fn issue_actor_token(state: &AppState, claims: &Claims) -> Result<String, String> {
    let actor_claims = build_access_claims(
        &state.jwt_config,
        claims.sub,
        &claims.email,
        &claims.name,
        claims.roles.clone(),
        claims.permissions.clone(),
        claims.org_id,
        claims.attributes.clone(),
        claims.auth_methods.clone(),
    );
    let token = encode_token(&state.jwt_config, &actor_claims)
        .map_err(|error| format!("failed to issue approval actor token: {error}"))?;
    Ok(format!("Bearer {token}"))
}

fn record_submitted_action(
    context: &mut Value,
    step_id: &str,
    invocation: &ResolvedActionInvocation,
    status: &str,
    response: Option<&Value>,
    error: Option<&str>,
) {
    let entry = json!({
        "step_id": step_id,
        "status": status,
        "action_id": invocation.action_id,
        "target_object_id": invocation.target_object_id,
        "parameters": invocation.parameters,
        "justification": invocation.justification,
        "execution_identity": invocation.execution_identity,
        "response": response,
        "error": error,
        "executed_at": Utc::now(),
    });

    set_collection_entry(context, "submitted_actions", step_id, entry.clone());
    ensure_object(context);
    context
        .as_object_mut()
        .expect("context must be object")
        .insert("last_submitted_action".to_string(), entry);
}

fn record_action_proposal(
    context: &mut Value,
    step_id: &str,
    proposal: &StoredActionProposal,
    status: &str,
    response: Option<&Value>,
    error: Option<&str>,
) {
    let entry = json!({
        "step_id": step_id,
        "status": status,
        "proposal": proposal,
        "response": response,
        "error": error,
        "updated_at": Utc::now(),
    });

    set_collection_entry(context, "action_proposals", step_id, entry.clone());
    ensure_object(context);
    context
        .as_object_mut()
        .expect("context must be object")
        .insert("last_action_proposal".to_string(), entry);
}

fn set_collection_entry(context: &mut Value, collection_key: &str, item_key: &str, value: Value) {
    ensure_object(context);
    let object = context.as_object_mut().expect("context must be object");
    let collection = object
        .entry(collection_key.to_string())
        .or_insert_with(|| Value::Object(Map::new()));
    ensure_object(collection);
    collection
        .as_object_mut()
        .expect("collection must be object")
        .insert(item_key.to_string(), value);
}

fn ensure_object(value: &mut Value) {
    if !value.is_object() {
        *value = Value::Object(Map::new());
    }
}
