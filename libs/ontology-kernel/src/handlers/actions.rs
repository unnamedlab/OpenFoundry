use std::collections::{HashMap, HashSet};
use std::time::Instant;

use auth_middleware::{
    claims::Claims,
    jwt::{build_access_claims, encode_token},
    layer::AuthUser,
};
use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::{IntoResponse, Response},
};
use chrono::Utc;
use reqwest::{Method, Url};
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value, json};
use storage_abstraction::repositories::{
    ActionLogEntry, LinkTypeId, MarkingId, Object, ObjectId, OwnerId, Page, ReadConsistency,
    TenantId, TypeId,
};
use uuid::Uuid;

use crate::{
    AppState,
    domain::{
        access::{clearance_rank, ensure_object_access, marking_rank, validate_marking},
        action_repository, composition,
        function_metrics::{FunctionPackageRunContext, record_function_package_run},
        function_runtime::{
            ResolvedInlineFunction, execute_inline_function, resolve_inline_function_config,
        },
        schema::validate_object_properties,
        type_system::{validate_property_type, validate_property_value},
        writeback,
    },
    models::{
        action_type::{
            ActionAuthorizationPolicy, ActionFormCondition, ActionFormSchema, ActionInputField,
            ActionOperationKind, ActionType, ActionTypeRow, ActionWhatIfBranch,
            CreateActionTypeRequest, CreateActionWhatIfBranchRequest, ExecuteActionRequest,
            ExecuteActionResponse, ExecuteBatchActionRequest, ExecuteBatchActionResponse,
            ListActionTypesQuery, ListActionTypesResponse, ListActionWhatIfBranchesQuery,
            ListActionWhatIfBranchesResponse, UpdateActionTypeRequest, UpdateObjectActionConfig,
            ValidateActionRequest, ValidateActionResponse,
        },
        link_type::LinkType,
        property::{ExecuteInlineEditRequest, Property, PropertyInlineEditConfig},
    },
};

use super::{
    links::LinkInstance,
    objects::{ObjectInstance, load_object_instance},
};

#[derive(Debug, Deserialize)]
struct CreateLinkActionConfig {
    link_type_id: Uuid,
    target_input_name: String,
    #[serde(default = "default_source_role")]
    source_role: String,
    properties_input_name: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
struct HttpInvocationConfig {
    url: String,
    #[serde(default = "default_http_method")]
    method: String,
    #[serde(default)]
    headers: HashMap<String, String>,
}

#[derive(Debug, Deserialize)]
struct FunctionLinkInstruction {
    link_type_id: Uuid,
    target_object_id: Uuid,
    #[serde(default = "default_source_role")]
    source_role: String,
    properties: Option<Value>,
}

#[derive(Debug, Clone, Deserialize, Default)]
struct ActionConfigEnvelope {
    #[serde(default)]
    operation: Option<Value>,
    #[serde(default)]
    notification_side_effects: Vec<ActionNotificationSideEffectConfig>,
    #[serde(default)]
    webhook_writeback: Option<WebhookCallConfig>,
    #[serde(default)]
    webhook_side_effects: Vec<WebhookCallConfig>,
    #[serde(default)]
    batched_execution: bool,
}

/// Reference to a webhook registered in `connector-management-service` plus
/// the parameter mapping that turns action inputs into the webhook's input
/// schema. TASK G — used for both writeback (synchronous, blocking) and
/// post-rules side effects (parallel, non-blocking).
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct WebhookCallConfig {
    pub webhook_id: Uuid,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub input_mappings: Vec<WebhookInputMapping>,
    /// When set, captured `output_parameters` are exposed under this key in the
    /// rule-evaluation context (only honoured for writeback).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub output_parameter_alias: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct WebhookInputMapping {
    pub webhook_input_name: String,
    /// Name of the action parameter (input field) that supplies the value.
    pub action_input_name: String,
}

#[derive(Debug, Clone, Deserialize)]
struct ActionNotificationSideEffectConfig {
    title: String,
    body: String,
    #[serde(default)]
    severity: Option<String>,
    #[serde(default)]
    category: Option<String>,
    #[serde(default)]
    channels: Option<Vec<String>>,
    #[serde(default)]
    user_ids: Vec<Uuid>,
    #[serde(default)]
    user_id_input_name: Option<String>,
    #[serde(default)]
    target_user_property_name: Option<String>,
    #[serde(default)]
    send_to_actor: bool,
    #[serde(default)]
    send_to_target_creator: bool,
    #[serde(default)]
    broadcast: bool,
    #[serde(default)]
    metadata: Option<Value>,
}

#[derive(Debug, Clone, Serialize)]
struct InternalSendNotificationRequest {
    user_id: Option<Uuid>,
    title: String,
    body: String,
    severity: Option<String>,
    category: Option<String>,
    channels: Option<Vec<String>>,
    metadata: Option<Value>,
}

#[derive(Debug, Clone)]
struct EffectiveActionInputField {
    definition: ActionInputField,
    hidden: bool,
}

enum ActionPlan {
    UpdateObject {
        target: ObjectInstance,
        patch: Map<String, Value>,
    },
    CreateLink {
        target: ObjectInstance,
        counterpart: ObjectInstance,
        link_type: LinkType,
        properties: Option<Value>,
        source_object_id: Uuid,
        target_object_id: Uuid,
    },
    DeleteObject {
        target: ObjectInstance,
    },
    InvokeFunction {
        target: Option<ObjectInstance>,
        invocation: FunctionInvocation,
        payload: Value,
        parameters: HashMap<String, Value>,
    },
    InvokeWebhook {
        target: Option<ObjectInstance>,
        invocation: HttpInvocationConfig,
        payload: Value,
    },
}

enum FunctionInvocation {
    Http(HttpInvocationConfig),
    Inline(ResolvedInlineFunction),
}

struct ExecutedAction {
    target_object_id: Option<Uuid>,
    deleted: bool,
    preview: Value,
    object: Option<Value>,
    link: Option<Value>,
    result: Option<Value>,
}

fn default_source_role() -> String {
    "source".to_string()
}

fn default_http_method() -> String {
    "POST".to_string()
}

fn tenant_from_claims(claims: &Claims) -> TenantId {
    TenantId(
        claims
            .org_id
            .map(|id| id.to_string())
            .unwrap_or_else(|| "default".to_string()),
    )
}

async fn persist_link_instance(
    state: &AppState,
    claims: &Claims,
    actor_id: Uuid,
    link_type: &LinkType,
    source_object_id: Uuid,
    target_object_id: Uuid,
    properties: Option<Value>,
    error_context: &str,
) -> Result<LinkInstance, String> {
    let created_at = Utc::now();
    let payload = properties.clone().unwrap_or_else(|| json!({}));
    composition::create_link(
        state.stores.links.as_ref(),
        tenant_from_claims(claims),
        LinkTypeId(link_type.id.to_string()),
        ObjectId(source_object_id.to_string()),
        ObjectId(target_object_id.to_string()),
        payload,
        created_at.timestamp_millis(),
    )
    .await
    .map_err(|e| format!("{error_context}: {e}"))?;

    Ok(LinkInstance {
        id: composition::stable_link_id(
            &LinkTypeId(link_type.id.to_string()),
            &ObjectId(source_object_id.to_string()),
            &ObjectId(target_object_id.to_string()),
        ),
        link_type_id: link_type.id,
        source_object_id,
        target_object_id,
        properties,
        created_by: actor_id,
        created_at,
    })
}

async fn delete_object_via_store(
    state: &AppState,
    claims: &Claims,
    object_id: Uuid,
    error_context: &str,
) -> Result<(), String> {
    let deleted = state
        .stores
        .objects
        .delete(
            &tenant_from_claims(claims),
            &ObjectId(object_id.to_string()),
        )
        .await
        .map_err(|error| format!("{error_context}: {error}"))?;

    if !deleted {
        return Err("target object no longer exists".to_string());
    }

    Ok(())
}

fn split_action_config(
    config: &Value,
) -> Result<(Value, Vec<ActionNotificationSideEffectConfig>), String> {
    let Some(config_object) = config.as_object() else {
        return Ok((config.clone(), Vec::new()));
    };

    if !config_object.contains_key("operation")
        && !config_object.contains_key("notification_side_effects")
        && !config_object.contains_key("webhook_writeback")
        && !config_object.contains_key("webhook_side_effects")
        && !config_object.contains_key("batched_execution")
    {
        return Ok((config.clone(), Vec::new()));
    }

    let envelope: ActionConfigEnvelope = serde_json::from_value(config.clone())
        .map_err(|error| format!("invalid action config envelope: {error}"))?;

    Ok((
        envelope.operation.unwrap_or(Value::Null),
        envelope.notification_side_effects,
    ))
}

/// TASK G — Returns the parsed envelope so callers can extract the new
/// webhook + batched-execution fields without re-doing the full split.
/// Returns `Ok(None)` for legacy configs that don't carry the envelope keys.
fn parse_action_envelope(config: &Value) -> Result<Option<ActionConfigEnvelope>, String> {
    let Some(config_object) = config.as_object() else {
        return Ok(None);
    };
    if !config_object.contains_key("operation")
        && !config_object.contains_key("notification_side_effects")
        && !config_object.contains_key("webhook_writeback")
        && !config_object.contains_key("webhook_side_effects")
        && !config_object.contains_key("batched_execution")
    {
        return Ok(None);
    }
    let envelope: ActionConfigEnvelope = serde_json::from_value(config.clone())
        .map_err(|error| format!("invalid action config envelope: {error}"))?;
    Ok(Some(envelope))
}

/// Convenience accessor for the writeback/side-effect webhook configs.
fn split_webhook_configs(
    config: &Value,
) -> Result<(Option<WebhookCallConfig>, Vec<WebhookCallConfig>), String> {
    match parse_action_envelope(config)? {
        Some(envelope) => Ok((envelope.webhook_writeback, envelope.webhook_side_effects)),
        None => Ok((None, Vec::new())),
    }
}

/// Convenience accessor for the `batched_execution` flag.
fn extract_batched_execution_flag(config: &Value) -> bool {
    parse_action_envelope(config)
        .ok()
        .flatten()
        .map(|envelope| envelope.batched_execution)
        .unwrap_or(false)
}

fn validate_notification_side_effect_configs(
    configs: &[ActionNotificationSideEffectConfig],
    input_names: &HashSet<&str>,
    property_types: &HashMap<&str, &str>,
) -> Result<(), String> {
    for (index, config) in configs.iter().enumerate() {
        if config.title.trim().is_empty() {
            return Err(format!(
                "notification_side_effects[{index}] title is required"
            ));
        }
        if config.body.trim().is_empty() {
            return Err(format!(
                "notification_side_effects[{index}] body is required"
            ));
        }
        if let Some(channels) = config.channels.as_ref() {
            if channels.is_empty() {
                return Err(format!(
                    "notification_side_effects[{index}] channels must not be empty"
                ));
            }
            if channels.iter().any(|channel| channel.trim().is_empty()) {
                return Err(format!(
                    "notification_side_effects[{index}] channels cannot contain empty values"
                ));
            }
        }
        if let Some(input_name) = config.user_id_input_name.as_deref() {
            if !input_names.contains(input_name) {
                return Err(format!(
                    "notification_side_effects[{index}] references unknown input field '{input_name}'"
                ));
            }
        }
        if let Some(property_name) = config.target_user_property_name.as_deref() {
            if !property_types.contains_key(property_name) {
                return Err(format!(
                    "notification_side_effects[{index}] references unknown target property '{property_name}'"
                ));
            }
        }
        if config.user_ids.is_empty()
            && config.user_id_input_name.is_none()
            && config.target_user_property_name.is_none()
            && !config.send_to_actor
            && !config.send_to_target_creator
            && !config.broadcast
        {
            return Err(format!(
                "notification_side_effects[{index}] must define at least one recipient source"
            ));
        }
    }

    Ok(())
}

/// TASK G — Verify a webhook config: webhook_id non-nil and every input
/// mapping references a declared action input. The actual webhook input
/// schema is fetched lazily from `connector-management-service`; we only
/// validate the parameter side here.
fn validate_webhook_call_config(
    config: &WebhookCallConfig,
    input_names: &HashSet<&str>,
    label: &str,
) -> Result<(), String> {
    if config.webhook_id.is_nil() {
        return Err(format!("{label}.webhook_id must not be nil"));
    }
    for mapping in &config.input_mappings {
        if mapping.webhook_input_name.trim().is_empty() {
            return Err(format!("{label}.webhook_input_name is required"));
        }
        if !input_names.contains(mapping.action_input_name.as_str()) {
            return Err(format!(
                "{label} references unknown action input '{}' (OntologyMetadata:ActionWebhookInputsDoNotHaveExpectedType)",
                mapping.action_input_name
            ));
        }
    }
    Ok(())
}

fn value_to_template_string(value: &Value) -> String {
    match value {
        Value::Null => String::new(),
        Value::String(value) => value.clone(),
        Value::Bool(value) => value.to_string(),
        Value::Number(value) => value.to_string(),
        _ => value.to_string(),
    }
}

fn lookup_template_value(context: &Value, path: &str) -> Option<String> {
    let mut current = context;
    for segment in path.split('.') {
        current = current.get(segment)?;
    }
    Some(value_to_template_string(current))
}

fn render_template(template: &str, context: &Value) -> String {
    let mut rendered = String::with_capacity(template.len());
    let mut remaining = template;

    while let Some(start) = remaining.find("{{") {
        rendered.push_str(&remaining[..start]);
        let after_start = &remaining[start + 2..];
        if let Some(end) = after_start.find("}}") {
            let token = after_start[..end].trim();
            if let Some(value) = lookup_template_value(context, token) {
                rendered.push_str(&value);
            }
            remaining = &after_start[end + 2..];
        } else {
            rendered.push_str(&remaining[start..]);
            return rendered;
        }
    }

    rendered.push_str(remaining);
    rendered
}

fn render_value_templates(value: &Value, context: &Value) -> Value {
    match value {
        Value::String(template) => Value::String(render_template(template, context)),
        Value::Array(items) => Value::Array(
            items
                .iter()
                .map(|item| render_value_templates(item, context))
                .collect(),
        ),
        Value::Object(map) => Value::Object(
            map.iter()
                .map(|(key, value)| (key.clone(), render_value_templates(value, context)))
                .collect(),
        ),
        _ => value.clone(),
    }
}

fn lookup_path_value<'a>(context: &'a Value, path: &str) -> Option<&'a Value> {
    let mut current = context;
    for segment in path.split('.') {
        current = current.get(segment)?;
    }
    Some(current)
}

fn validate_action_form_condition(
    condition: &ActionFormCondition,
    input_names: &HashSet<&str>,
    property_types: &HashMap<&str, &str>,
    scope: &str,
) -> Result<(), String> {
    if condition.left.trim().is_empty() {
        return Err(format!("{scope} condition left path is required"));
    }

    if let Some(parameter_name) = condition.left.strip_prefix("parameters.") {
        if parameter_name.trim().is_empty() || !input_names.contains(parameter_name) {
            return Err(format!(
                "{scope} references unknown parameter path '{}'",
                condition.left
            ));
        }
    } else if let Some(property_name) = condition.left.strip_prefix("target.properties.") {
        if property_name.trim().is_empty() || !property_types.contains_key(property_name) {
            return Err(format!(
                "{scope} references unknown target property path '{}'",
                condition.left
            ));
        }
    } else if !matches!(
        condition.left.as_str(),
        "target.id" | "target.created_by" | "target.marking"
    ) {
        return Err(format!(
            "{scope} uses unsupported condition path '{}'",
            condition.left
        ));
    }

    match condition.operator.as_str() {
        "exists" | "not_exists" => Ok(()),
        "is"
        | "is_not"
        | "includes"
        | "greater_than"
        | "greater_than_or_equals"
        | "less_than"
        | "less_than_or_equals" => {
            if condition.right.is_none() {
                Err(format!(
                    "{scope} operator '{}' requires a right-hand value",
                    condition.operator
                ))
            } else {
                Ok(())
            }
        }
        other => Err(format!("{scope} uses unsupported operator '{}'", other)),
    }
}

fn validate_action_form_schema(
    form_schema: &ActionFormSchema,
    input_schema: &[ActionInputField],
    property_types: &HashMap<&str, &str>,
) -> Result<(), String> {
    let input_names = input_schema
        .iter()
        .map(|field| field.name.as_str())
        .collect::<HashSet<_>>();
    let input_types = input_schema
        .iter()
        .map(|field| (field.name.as_str(), field.property_type.as_str()))
        .collect::<HashMap<_, _>>();
    let mut seen_sections = HashSet::new();
    let mut assigned_parameters = HashSet::new();

    for (index, section) in form_schema.sections.iter().enumerate() {
        if section.id.trim().is_empty() {
            return Err(format!("form_schema.sections[{index}] id is required"));
        }
        if !seen_sections.insert(section.id.as_str()) {
            return Err(format!(
                "form_schema.sections[{index}] duplicates section id '{}'",
                section.id
            ));
        }
        if !(1..=2).contains(&section.columns) {
            return Err(format!(
                "form_schema.sections[{index}] columns must be 1 or 2"
            ));
        }
        for parameter_name in &section.parameter_names {
            if !input_names.contains(parameter_name.as_str()) {
                return Err(format!(
                    "form_schema.sections[{index}] references unknown parameter '{}'",
                    parameter_name
                ));
            }
            if !assigned_parameters.insert(parameter_name.as_str()) {
                return Err(format!(
                    "parameter '{}' is assigned to more than one form section",
                    parameter_name
                ));
            }
        }
        for (override_index, section_override) in section.overrides.iter().enumerate() {
            if let Some(columns) = section_override.columns {
                if !(1..=2).contains(&columns) {
                    return Err(format!(
                        "form_schema.sections[{index}].overrides[{override_index}] columns must be 1 or 2"
                    ));
                }
            }
            for (condition_index, condition) in section_override.conditions.iter().enumerate() {
                validate_action_form_condition(
                    condition,
                    &input_names,
                    property_types,
                    &format!(
                        "form_schema.sections[{index}].overrides[{override_index}].conditions[{condition_index}]"
                    ),
                )?;
            }
        }
    }

    for (index, parameter_override) in form_schema.parameter_overrides.iter().enumerate() {
        if parameter_override.parameter_name.trim().is_empty() {
            return Err(format!(
                "form_schema.parameter_overrides[{index}] parameter_name is required"
            ));
        }
        let property_type = input_types
            .get(parameter_override.parameter_name.as_str())
            .ok_or_else(|| {
                format!(
                    "form_schema.parameter_overrides[{index}] references unknown parameter '{}'",
                    parameter_override.parameter_name
                )
            })?;
        if let Some(default_value) = parameter_override.default_value.as_ref() {
            validate_property_value(property_type, default_value).map_err(|error| {
                format!("form_schema.parameter_overrides[{index}].default_value: {error}")
            })?;
        }
        for (condition_index, condition) in parameter_override.conditions.iter().enumerate() {
            validate_action_form_condition(
                condition,
                &input_names,
                property_types,
                &format!("form_schema.parameter_overrides[{index}].conditions[{condition_index}]"),
            )?;
        }
    }

    Ok(())
}

fn build_action_form_context(
    parameters: &HashMap<String, Value>,
    target: Option<&ObjectInstance>,
) -> Value {
    let target_value = target.map(|object| {
        json!({
            "id": object.id,
            "created_by": object.created_by,
            "marking": object.marking,
            "properties": object.properties,
        })
    });

    json!({
        "parameters": parameters,
        "target": target_value,
    })
}

fn value_exists(value: Option<&Value>) -> bool {
    value.is_some_and(|candidate| !candidate.is_null())
}

fn compare_numeric_values(left: &Value, right: &Value) -> Option<std::cmp::Ordering> {
    let left = left.as_f64()?;
    let right = right.as_f64()?;
    left.partial_cmp(&right)
}

fn matches_action_form_condition(condition: &ActionFormCondition, context: &Value) -> bool {
    let left = lookup_path_value(context, &condition.left);
    match condition.operator.as_str() {
        "exists" => value_exists(left),
        "not_exists" => !value_exists(left),
        "is" => left == condition.right.as_ref(),
        "is_not" => left != condition.right.as_ref(),
        "includes" => match (left, condition.right.as_ref()) {
            (Some(Value::Array(items)), Some(right)) => items.iter().any(|item| item == right),
            (Some(Value::String(left)), Some(Value::String(right))) => left.contains(right),
            _ => false,
        },
        "greater_than" => matches!(
            (left, condition.right.as_ref()),
            (Some(left), Some(right))
                if compare_numeric_values(left, right) == Some(std::cmp::Ordering::Greater)
        ),
        "greater_than_or_equals" => matches!(
            (left, condition.right.as_ref()),
            (Some(left), Some(right))
                if matches!(
                    compare_numeric_values(left, right),
                    Some(std::cmp::Ordering::Greater) | Some(std::cmp::Ordering::Equal)
                )
        ),
        "less_than" => matches!(
            (left, condition.right.as_ref()),
            (Some(left), Some(right))
                if compare_numeric_values(left, right) == Some(std::cmp::Ordering::Less)
        ),
        "less_than_or_equals" => matches!(
            (left, condition.right.as_ref()),
            (Some(left), Some(right))
                if matches!(
                    compare_numeric_values(left, right),
                    Some(std::cmp::Ordering::Less) | Some(std::cmp::Ordering::Equal)
                )
        ),
        _ => false,
    }
}

fn matches_action_form_conditions(conditions: &[ActionFormCondition], context: &Value) -> bool {
    conditions
        .iter()
        .all(|condition| matches_action_form_condition(condition, context))
}

fn resolve_effective_input_schema(
    input_schema: &[ActionInputField],
    form_schema: &ActionFormSchema,
    parameters: &HashMap<String, Value>,
    target: Option<&ObjectInstance>,
) -> Vec<EffectiveActionInputField> {
    let context = build_action_form_context(parameters, target);

    input_schema
        .iter()
        .cloned()
        .map(|field| {
            let mut resolved = field;
            let mut hidden = false;

            if let Some(parameter_override) =
                form_schema
                    .parameter_overrides
                    .iter()
                    .find(|override_block| {
                        override_block.parameter_name == resolved.name
                            && matches_action_form_conditions(&override_block.conditions, &context)
                    })
            {
                hidden = parameter_override.hidden.unwrap_or(false);
                if let Some(required) = parameter_override.required {
                    resolved.required = required;
                }
                if let Some(default_value) = parameter_override.default_value.clone() {
                    resolved.default_value = Some(default_value);
                }
                if parameter_override.display_name.is_some() {
                    resolved.display_name = parameter_override.display_name.clone();
                }
                if parameter_override.description.is_some() {
                    resolved.description = parameter_override.description.clone();
                }
            }

            EffectiveActionInputField {
                definition: resolved,
                hidden,
            }
        })
        .collect()
}

fn extract_uuid_values(value: &Value, source: &str) -> Result<Vec<Uuid>, String> {
    match value {
        Value::String(raw) => Uuid::parse_str(raw)
            .map(|uuid| vec![uuid])
            .map_err(|_| format!("{source} must contain UUID strings")),
        Value::Array(items) => items
            .iter()
            .map(|item| {
                item.as_str()
                    .ok_or_else(|| format!("{source} must contain UUID strings"))
                    .and_then(|raw| {
                        Uuid::parse_str(raw)
                            .map_err(|_| format!("{source} must contain UUID strings"))
                    })
            })
            .collect(),
        Value::Null => Ok(Vec::new()),
        _ => Err(format!(
            "{source} must be a UUID string or list of UUID strings"
        )),
    }
}

fn build_notification_metadata(
    config: &ActionNotificationSideEffectConfig,
    context: &Value,
    action: &ActionType,
    executed: &ExecutedAction,
) -> Value {
    let custom = config
        .metadata
        .as_ref()
        .map(|value| render_value_templates(value, context))
        .unwrap_or_else(|| json!({}));

    match custom {
        Value::Object(mut object) => {
            object.insert("action_id".to_string(), json!(action.id));
            object.insert("action_name".to_string(), json!(action.name));
            object.insert("operation_kind".to_string(), json!(action.operation_kind));
            object.insert(
                "target_object_id".to_string(),
                json!(executed.target_object_id),
            );
            object.insert("deleted".to_string(), json!(executed.deleted));
            Value::Object(object)
        }
        other => json!({
            "action_id": action.id,
            "action_name": action.name,
            "operation_kind": action.operation_kind,
            "target_object_id": executed.target_object_id,
            "deleted": executed.deleted,
            "custom": other,
        }),
    }
}

fn invalid_action(message: impl Into<String>) -> Response {
    (
        StatusCode::BAD_REQUEST,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

fn db_error(message: impl Into<String>) -> Response {
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

fn forbidden(message: impl Into<String>) -> Response {
    (
        StatusCode::FORBIDDEN,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

/// TASK M — Scale & property limit response helper. Returns HTTP 429 with a
/// `failure_type: "scale_limit"` body so the metrics pipeline classifies the
/// failure consistently with `FailureType::ScaleLimit` and the documented
/// Foundry scale limits (`Scale and property limits.md`).
fn scale_limit_response(message: impl Into<String>) -> Response {
    (
        StatusCode::TOO_MANY_REQUESTS,
        Json(json!({
            "error": message.into(),
            "failure_type": crate::metrics::FailureType::ScaleLimit.as_str(),
        })),
    )
        .into_response()
}

fn validate_action_authorization_policy(policy: &ActionAuthorizationPolicy) -> Result<(), String> {
    for permission_key in &policy.required_permission_keys {
        if permission_key.trim().is_empty() {
            return Err(
                "authorization_policy.required_permission_keys cannot contain empty values"
                    .to_string(),
            );
        }
    }
    for role in &policy.any_role {
        if role.trim().is_empty() {
            return Err("authorization_policy.any_role cannot contain empty values".to_string());
        }
    }
    for role in &policy.all_roles {
        if role.trim().is_empty() {
            return Err("authorization_policy.all_roles cannot contain empty values".to_string());
        }
    }
    for key in policy.attribute_equals.keys() {
        if key.trim().is_empty() {
            return Err(
                "authorization_policy.attribute_equals cannot contain empty keys".to_string(),
            );
        }
    }
    for marking in &policy.allowed_markings {
        validate_marking(marking)?;
    }
    if let Some(minimum_clearance) = policy.minimum_clearance.as_deref() {
        validate_marking(minimum_clearance)?;
    }

    Ok(())
}

pub(crate) fn ensure_action_actor_permission(
    claims: &Claims,
    action: &ActionType,
) -> Result<(), String> {
    if let Some(permission_key) = action.permission_key.as_deref() {
        if !claims.has_permission_key(permission_key) {
            return Err(format!(
                "forbidden: missing permission '{}'",
                permission_key
            ));
        }
    }

    for permission_key in &action.authorization_policy.required_permission_keys {
        if !claims.has_permission_key(permission_key) {
            return Err(format!(
                "forbidden: missing permission '{}'",
                permission_key
            ));
        }
    }

    if !action.authorization_policy.any_role.is_empty()
        && !action
            .authorization_policy
            .any_role
            .iter()
            .any(|role| claims.has_role(role))
    {
        return Err(format!(
            "forbidden: requires one of roles [{}]",
            action.authorization_policy.any_role.join(", ")
        ));
    }

    for role in &action.authorization_policy.all_roles {
        if !claims.has_role(role) {
            return Err(format!("forbidden: missing role '{}'", role));
        }
    }

    for (attribute_key, expected_value) in &action.authorization_policy.attribute_equals {
        match claims.attribute(attribute_key) {
            Some(actual_value) if actual_value == expected_value => {}
            Some(_) => {
                return Err(format!(
                    "forbidden: attribute '{}' does not satisfy the action policy",
                    attribute_key
                ));
            }
            None => {
                return Err(format!(
                    "forbidden: missing required attribute '{}'",
                    attribute_key
                ));
            }
        }
    }

    if action.authorization_policy.deny_guest_sessions && claims.is_guest_session() {
        return Err("forbidden: guest sessions may not execute this action".to_string());
    }

    if let Some(minimum_clearance) = action.authorization_policy.minimum_clearance.as_deref() {
        let required_clearance = marking_rank(minimum_clearance).ok_or_else(|| {
            format!(
                "forbidden: invalid action minimum_clearance '{}'",
                minimum_clearance
            )
        })?;
        if clearance_rank(claims) < required_clearance {
            return Err(format!(
                "forbidden: action requires '{}' classification clearance",
                minimum_clearance
            ));
        }
    }

    Ok(())
}

pub(crate) fn ensure_action_target_permission(
    action: &ActionType,
    target: Option<&ObjectInstance>,
) -> Result<(), String> {
    if action.authorization_policy.allowed_markings.is_empty() {
        return Ok(());
    }

    let Some(target) = target else {
        return Err("target object is required by the action authorization policy".to_string());
    };

    if action
        .authorization_policy
        .allowed_markings
        .iter()
        .any(|marking| marking.eq_ignore_ascii_case(&target.marking))
    {
        Ok(())
    } else {
        Err(format!(
            "forbidden: target marking '{}' is not allowed for this action",
            target.marking
        ))
    }
}

fn ensure_confirmation_justification(
    action: &ActionType,
    justification: Option<&str>,
) -> Result<(), String> {
    if action.confirmation_required
        && justification
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .is_none()
    {
        return Err("justification is required for confirmation_required actions".to_string());
    }

    Ok(())
}

fn all_forbidden(errors: &[String]) -> bool {
    !errors.is_empty() && errors.iter().all(|error| error.starts_with("forbidden:"))
}

async fn load_action_row(
    state: &AppState,
    action_id: Uuid,
) -> Result<Option<ActionTypeRow>, String> {
    action_repository::get_action_row(state.stores.definitions.as_ref(), action_id)
        .await
        .map_err(|error| error.to_string())
}

async fn load_property_row(
    state: &AppState,
    object_type_id: Uuid,
    property_id: Uuid,
) -> Result<Option<Property>, String> {
    action_repository::load_property_for_object_type(
        state.stores.definitions.as_ref(),
        object_type_id,
        property_id,
    )
    .await
    .map_err(|error| error.to_string())
}

async fn ensure_object_type_exists(state: &AppState, object_type_id: Uuid) -> Result<bool, String> {
    action_repository::object_type_exists(state.stores.definitions.as_ref(), object_type_id)
        .await
        .map_err(|error| error.to_string())
}

fn parse_operation_kind(raw: &str) -> Result<ActionOperationKind, String> {
    serde_json::from_value::<ActionOperationKind>(Value::String(raw.to_string()))
        .map_err(|_| format!("invalid action operation kind '{raw}'"))
}

fn ensure_input_schema(input_schema: &[ActionInputField]) -> Result<(), String> {
    let mut seen = HashSet::new();
    for field in input_schema {
        if field.name.trim().is_empty() {
            return Err("action input field name is required".to_string());
        }
        if !seen.insert(field.name.clone()) {
            return Err(format!("duplicate action input field '{}'", field.name));
        }
        validate_property_type(&field.property_type)?;
        if let Some(default_value) = &field.default_value {
            validate_property_value(&field.property_type, default_value)?;
        }
        // TASK J — Struct parameters require nested `struct_fields`; other
        // property types must NOT carry them. Sub-fields are themselves
        // validated recursively (no nested structs of structs allowed since
        // the supported sub-field base types are scalar — Foundry rule).
        ensure_struct_fields_consistency(field)?;
    }
    Ok(())
}

/// TASK J — Validate the `struct_fields` slot relative to the parent
/// field's property_type.
///
/// Constraints derived from `Actions on structs.md`:
/// * `property_type == "struct"` ⇒ `struct_fields` must exist and be
///   non-empty.
/// * Other property types ⇒ `struct_fields` must be `None`.
/// * Sub-field base types are restricted to scalars (`boolean`, `date`,
///   `float`, `geo_point`, `integer`, `string`, `timestamp`). Nested
///   `struct` is rejected.
fn ensure_struct_fields_consistency(field: &ActionInputField) -> Result<(), String> {
    match (field.property_type.as_str(), field.struct_fields.as_ref()) {
        ("struct", None) => Err(format!(
            "struct parameter '{}' must declare struct_fields",
            field.name
        )),
        ("struct", Some(sub_fields)) if sub_fields.is_empty() => Err(format!(
            "struct parameter '{}' must declare at least one sub-field",
            field.name
        )),
        ("struct", Some(sub_fields)) => {
            let mut seen = HashSet::new();
            for sub_field in sub_fields {
                if sub_field.name.trim().is_empty() {
                    return Err(format!(
                        "struct parameter '{}' has a sub-field with empty name",
                        field.name
                    ));
                }
                if !seen.insert(sub_field.name.clone()) {
                    return Err(format!(
                        "struct parameter '{}' has duplicate sub-field '{}'",
                        field.name, sub_field.name
                    ));
                }
                if sub_field.property_type == "struct" {
                    return Err(format!(
                        "struct parameter '{}' sub-field '{}' cannot itself be a struct",
                        field.name, sub_field.name
                    ));
                }
                validate_property_type(&sub_field.property_type)?;
                if let Some(default_value) = &sub_field.default_value {
                    validate_property_value(&sub_field.property_type, default_value).map_err(
                        |error| {
                            format!(
                                "struct parameter '{}' sub-field '{}' default_value: {error}",
                                field.name, sub_field.name
                            )
                        },
                    )?;
                }
                if sub_field.struct_fields.is_some() {
                    return Err(format!(
                        "struct parameter '{}' sub-field '{}' must not declare struct_fields",
                        field.name, sub_field.name
                    ));
                }
            }
            Ok(())
        }
        (_, Some(_)) => Err(format!(
            "non-struct parameter '{}' must not declare struct_fields",
            field.name
        )),
        (_, None) => Ok(()),
    }
}

/// TASK J — Recursively validate a struct parameter value against its
/// declared sub-fields. Returns one error per offending sub-field so the
/// caller can prefix them with the parent parameter name.
fn validate_struct_parameter_value(
    field: &ActionInputField,
    value: &Value,
) -> Result<(), Vec<String>> {
    let Some(object) = value.as_object() else {
        return Err(vec!["expected object value for struct".to_string()]);
    };
    let Some(sub_fields) = field.struct_fields.as_ref() else {
        return Ok(());
    };

    let known = sub_fields
        .iter()
        .map(|sub_field| sub_field.name.as_str())
        .collect::<HashSet<_>>();
    let mut errors = Vec::new();

    for key in object.keys() {
        if !known.contains(key.as_str()) {
            errors.push(format!("unknown struct sub-field '{key}'"));
        }
    }

    for sub_field in sub_fields {
        let provided = object.get(&sub_field.name);
        let resolved = provided
            .cloned()
            .or_else(|| sub_field.default_value.clone());
        match resolved {
            Some(value) => {
                if let Err(error) = validate_property_value(&sub_field.property_type, &value) {
                    errors.push(format!("{}: {}", sub_field.name, error));
                }
            }
            None if sub_field.required => {
                errors.push(format!("{} is required", sub_field.name));
            }
            None => {}
        }
    }

    if errors.is_empty() {
        Ok(())
    } else {
        Err(errors)
    }
}

fn ensure_object_type_match(object: &ObjectInstance, expected_type: Uuid) -> Result<(), String> {
    if object.object_type_id == expected_type {
        Ok(())
    } else {
        Err("target object does not belong to the action object type".to_string())
    }
}

fn materialize_parameters(
    input_schema: &[ActionInputField],
    parameters: &Value,
    form_schema: &ActionFormSchema,
    target: Option<&ObjectInstance>,
) -> Result<HashMap<String, Value>, Vec<String>> {
    let provided = parameters.as_object().cloned().unwrap_or_default();
    let provided_parameters = provided
        .iter()
        .map(|(key, value)| (key.clone(), value.clone()))
        .collect::<HashMap<_, _>>();
    let effective_input_schema =
        resolve_effective_input_schema(input_schema, form_schema, &provided_parameters, target);

    let mut values = HashMap::new();
    let mut errors = Vec::new();

    for effective_field in effective_input_schema {
        let EffectiveActionInputField {
            definition: field,
            hidden,
        } = effective_field;
        let value = provided
            .get(&field.name)
            .cloned()
            .or_else(|| field.default_value.clone());

        match value {
            Some(value) => {
                if let Err(error) = validate_property_value(&field.property_type, &value) {
                    errors.push(format!("{}: {}", field.name, error));
                } else if field.property_type == "struct" {
                    // TASK J — Recursive validation of struct sub-fields.
                    if let Err(struct_errors) = validate_struct_parameter_value(&field, &value) {
                        for error in struct_errors {
                            errors.push(format!("{}.{}", field.name, error));
                        }
                    } else {
                        values.insert(field.name.clone(), value);
                    }
                } else {
                    values.insert(field.name.clone(), value);
                }
            }
            // TASK K — Hidden parameters with no provided value (and no
            // default) are skipped silently. Required only matters when the
            // parameter is visible. Mirrors the Foundry "hidden + no default
            // ⇒ skip" rule from `Override parameter configurations.md`.
            None if field.required && !hidden => {
                errors.push(format!("{} is required", field.name));
            }
            None => {}
        }
    }

    if errors.is_empty() {
        Ok(values)
    } else {
        Err(errors)
    }
}

fn resolve_inline_edit_input_name(
    action: &ActionType,
    property_name: &str,
    inline_edit_config: &PropertyInlineEditConfig,
) -> Result<String, String> {
    let (operation_config, _) = split_action_config(&action.config)?;
    let update_config: UpdateObjectActionConfig = serde_json::from_value(operation_config)
        .map_err(|error| format!("invalid inline edit action config: {error}"))?;

    let candidates = update_config
        .property_mappings
        .into_iter()
        .filter(|mapping| mapping.property_name == property_name)
        .filter_map(|mapping| mapping.input_name)
        .collect::<Vec<_>>();

    if let Some(input_name) = inline_edit_config.input_name.as_deref() {
        if candidates.iter().any(|candidate| candidate == input_name) {
            return Ok(input_name.to_string());
        }
        return Err(format!(
            "inline edit action does not map property '{property_name}' from input '{input_name}'"
        ));
    }

    let unique_candidates = candidates.into_iter().collect::<HashSet<_>>();
    match unique_candidates.len() {
        0 => Err(format!(
            "inline edit action must map property '{property_name}' from an input field"
        )),
        1 => Ok(unique_candidates.into_iter().next().unwrap_or_default()),
        _ => Err(format!(
            "inline edit action maps property '{property_name}' from multiple input fields; configure inline_edit_config.input_name explicitly"
        )),
    }
}

fn build_inline_edit_parameters(
    action: &ActionType,
    property: &Property,
    target: &ObjectInstance,
    inline_edit_config: &PropertyInlineEditConfig,
    value: Value,
) -> Result<Value, String> {
    let editable_input_name =
        resolve_inline_edit_input_name(action, &property.name, inline_edit_config)?;
    let (operation_config, _) = split_action_config(&action.config)?;
    let update_config: UpdateObjectActionConfig = serde_json::from_value(operation_config)
        .map_err(|error| format!("invalid inline edit action config: {error}"))?;

    let mut parameters = Map::new();
    parameters.insert(editable_input_name.clone(), value);

    for mapping in update_config.property_mappings {
        let Some(input_name) = mapping.input_name else {
            continue;
        };
        if input_name == editable_input_name || parameters.contains_key(&input_name) {
            continue;
        }
        if let Some(current_value) = target.properties.get(&mapping.property_name) {
            parameters.insert(input_name, current_value.clone());
        }
    }

    Ok(Value::Object(parameters))
}

fn resolve_uuid_parameter(
    parameters: &HashMap<String, Value>,
    field_name: &str,
) -> Result<Uuid, String> {
    let value = parameters
        .get(field_name)
        .and_then(Value::as_str)
        .ok_or_else(|| format!("{field_name} must be a UUID string"))?;

    Uuid::parse_str(value).map_err(|_| format!("{field_name} must be a valid UUID"))
}

fn resolve_notification_recipients(
    config: &ActionNotificationSideEffectConfig,
    parameters: &HashMap<String, Value>,
    target: Option<&ObjectInstance>,
    actor_id: Uuid,
) -> Result<(Vec<Uuid>, bool), String> {
    let mut recipients = HashSet::new();

    for user_id in &config.user_ids {
        recipients.insert(*user_id);
    }

    if let Some(input_name) = config.user_id_input_name.as_deref() {
        let value = parameters
            .get(input_name)
            .ok_or_else(|| format!("notification recipient input '{input_name}' is missing"))?;
        for user_id in extract_uuid_values(value, input_name)? {
            recipients.insert(user_id);
        }
    }

    if let Some(property_name) = config.target_user_property_name.as_deref() {
        let Some(target) = target else {
            return Err(format!(
                "notification recipient target property '{property_name}' requires a target object"
            ));
        };
        let value = target
            .properties
            .get(property_name)
            .ok_or_else(|| format!("target property '{property_name}' is missing"))?;
        for user_id in extract_uuid_values(value, property_name)? {
            recipients.insert(user_id);
        }
    }

    if config.send_to_actor {
        recipients.insert(actor_id);
    }

    if config.send_to_target_creator {
        let Some(target) = target else {
            return Err(
                "send_to_target_creator requires a target object for notification side effect"
                    .to_string(),
            );
        };
        recipients.insert(target.created_by);
    }

    if recipients.is_empty() && !config.broadcast {
        return Err("notification side effect resolved no recipients".to_string());
    }

    // TASK M — Cap notification fan-out per Foundry scale limits. The
    // stricter cap applies when the recipient list comes from a function
    // (i.e., resolved through a target user-property; Foundry calls this
    // recipient mode "From a function"). All other modes use the standard
    // 500-recipient ceiling.
    let from_function = config.target_user_property_name.is_some();
    let recipient_cap = if from_function {
        scale_limits::MAX_NOTIFICATION_RECIPIENTS_FROM_FUNCTION
    } else {
        scale_limits::MAX_NOTIFICATION_RECIPIENTS
    };
    if recipients.len() > recipient_cap {
        return Err(format!(
            "notification side effect resolved {} recipients which exceeds the scale limit ({} max{})",
            recipients.len(),
            recipient_cap,
            if from_function {
                ", recipients from a function"
            } else {
                ""
            }
        ));
    }

    let mut recipients = recipients.into_iter().collect::<Vec<_>>();
    recipients.sort_unstable();

    Ok((recipients, config.broadcast))
}

fn validate_notification_resolution(
    configs: &[ActionNotificationSideEffectConfig],
    parameters: &HashMap<String, Value>,
    target: Option<&ObjectInstance>,
    actor_id: Uuid,
) -> Result<(), String> {
    for config in configs {
        resolve_notification_recipients(config, parameters, target, actor_id)?;
    }

    Ok(())
}

fn validate_http_invocation_config(config: &Value) -> Result<HttpInvocationConfig, String> {
    let mut invocation: HttpInvocationConfig = serde_json::from_value(config.clone())
        .map_err(|e| format!("invalid HTTP action config: {e}"))?;

    if invocation.url.trim().is_empty() {
        return Err("HTTP action config requires a non-empty url".to_string());
    }

    let parsed_url = Url::parse(&invocation.url)
        .map_err(|e| format!("invalid HTTP action url '{}': {e}", invocation.url))?;
    if parsed_url.scheme() != "http" && parsed_url.scheme() != "https" {
        return Err("HTTP action url must use http or https".to_string());
    }

    invocation.method = invocation.method.trim().to_uppercase();
    let method = Method::from_bytes(invocation.method.as_bytes())
        .map_err(|_| format!("invalid HTTP action method '{}'", invocation.method))?;
    if !matches!(method, Method::POST | Method::PUT | Method::PATCH) {
        return Err("HTTP action method must be POST, PUT, or PATCH".to_string());
    }

    Ok(invocation)
}

fn build_http_payload(
    action: &ActionType,
    target: Option<&ObjectInstance>,
    parameters: &HashMap<String, Value>,
) -> Value {
    json!({
        "action": {
            "id": action.id,
            "name": &action.name,
            "display_name": &action.display_name,
            "object_type_id": action.object_type_id,
            "operation_kind": &action.operation_kind,
        },
        "target_object": target,
        "parameters": parameters,
    })
}

async fn invoke_http_action(
    state: &AppState,
    invocation: &HttpInvocationConfig,
    payload: &Value,
) -> Result<Value, String> {
    let method = Method::from_bytes(invocation.method.as_bytes())
        .map_err(|_| format!("invalid HTTP action method '{}'", invocation.method))?;
    let url = Url::parse(&invocation.url)
        .map_err(|e| format!("invalid HTTP action url '{}': {e}", invocation.url))?;

    let mut request = state.http_client.request(method, url);
    for (header_name, header_value) in &invocation.headers {
        request = request.header(header_name, header_value);
    }

    let response = request
        .json(payload)
        .send()
        .await
        .map_err(|e| format!("HTTP action request failed: {e}"))?;
    let status = response.status();
    let text = response
        .text()
        .await
        .map_err(|e| format!("failed to read HTTP action response: {e}"))?;

    if !status.is_success() {
        let detail = if text.trim().is_empty() {
            status.to_string()
        } else {
            text.clone()
        };
        return Err(format!("HTTP action returned {}: {}", status, detail));
    }

    if text.trim().is_empty() {
        Ok(Value::Null)
    } else {
        Ok(serde_json::from_str(&text).unwrap_or(Value::String(text)))
    }
}

async fn apply_object_patch(
    state: &AppState,
    claims: &Claims,
    target: &ObjectInstance,
    patch_value: &Value,
) -> Result<ObjectInstance, String> {
    let patch = patch_value
        .as_object()
        .ok_or_else(|| "object_patch must be a JSON object".to_string())?;
    let definitions = action_repository::load_effective_properties(
        state.stores.definitions.as_ref(),
        target.object_type_id,
    )
    .await
    .map_err(|e| format!("failed to load property definitions: {e}"))?;
    let property_types = definitions
        .iter()
        .map(|property| (property.name.as_str(), property.property_type.as_str()))
        .collect::<HashMap<_, _>>();
    let tenant = tenant_from_claims(claims);
    let object_id = ObjectId(target.id.to_string());
    let current = state
        .stores
        .objects
        .get(&tenant, &object_id, ReadConsistency::Strong)
        .await
        .map_err(|e| format!("failed to load current object version: {e}"))?;

    let mut next_properties = current
        .as_ref()
        .and_then(|object| object.payload.as_object().cloned())
        .or_else(|| target.properties.as_object().cloned())
        .unwrap_or_default();
    for (property_name, value) in patch {
        let property_type = property_types
            .get(property_name.as_str())
            .ok_or_else(|| format!("unknown property '{property_name}' in object_patch"))?;
        validate_property_value(property_type, value)
            .map_err(|e| format!("{}: {}", property_name, e))?;
        next_properties.insert(property_name.clone(), value.clone());
    }

    let normalized = validate_object_properties(&definitions, &Value::Object(next_properties))?;
    let expected_version = current.as_ref().map(|object| object.version);
    let next_version = expected_version.map(|version| version + 1).unwrap_or(1);
    let updated_at = Utc::now();
    let owner = current
        .as_ref()
        .and_then(|object| object.owner.clone())
        .or_else(|| Some(OwnerId(target.created_by.to_string())));
    let markings = current
        .as_ref()
        .map(|object| object.markings.clone())
        .filter(|markings| !markings.is_empty())
        .unwrap_or_else(|| vec![MarkingId(target.marking.clone())]);

    let updated_properties = normalized;
    let object = Object {
        tenant: tenant.clone(),
        id: object_id,
        type_id: TypeId(target.object_type_id.to_string()),
        version: next_version,
        payload: updated_properties.clone(),
        organization_id: target.organization_id.map(|id| id.to_string()),
        created_at_ms: Some(target.created_at.timestamp_millis()),
        updated_at_ms: updated_at.timestamp_millis(),
        owner,
        markings,
    };
    let event_payload = json!({
        "object_id": target.id,
        "object_type_id": target.object_type_id,
        "patch": patch_value,
        "properties": updated_properties.clone(),
        "actor_id": claims.sub,
        "organization_id": target.organization_id,
        "marking": target.marking.clone(),
        "version": next_version,
    });

    writeback::apply_object_with_outbox(
        &state.db,
        state.stores.objects.as_ref(),
        object,
        expected_version,
        "object",
        "ontology.object.changed.v1",
        event_payload,
    )
    .await
    .map_err(|e| format!("failed to apply object patch via Cassandra writeback: {e}"))?;

    Ok(ObjectInstance {
        id: target.id,
        object_type_id: target.object_type_id,
        properties: updated_properties,
        created_by: target.created_by,
        organization_id: target.organization_id,
        marking: target.marking.clone(),
        created_at: target.created_at,
        updated_at,
    })
}

async fn create_link_from_instruction(
    state: &AppState,
    claims: &Claims,
    actor_id: Uuid,
    target: &ObjectInstance,
    instruction: &FunctionLinkInstruction,
) -> Result<LinkInstance, String> {
    let counterpart = load_object_instance(
        state,
        claims,
        instruction.target_object_id,
        ReadConsistency::Strong,
    )
    .await
    .map_err(|e| format!("failed to load linked object: {e}"))?
    .ok_or_else(|| "linked object was not found".to_string())?;
    ensure_object_access(claims, &counterpart)?;

    let link_type = action_repository::load_link_type(
        state.stores.definitions.as_ref(),
        instruction.link_type_id,
    )
    .await
    .map_err(|e| format!("failed to load link type: {e}"))?
    .ok_or_else(|| "configured link type was not found".to_string())?;

    let expected_target_type = if instruction.source_role == "source" {
        link_type.source_type_id
    } else {
        link_type.target_type_id
    };
    if target.object_type_id != expected_target_type {
        return Err("target object does not match configured link endpoint".to_string());
    }

    let (source_object_id, target_object_id, expected_counterpart_type) =
        if instruction.source_role == "source" {
            (target.id, counterpart.id, link_type.target_type_id)
        } else {
            (counterpart.id, target.id, link_type.source_type_id)
        };

    if counterpart.object_type_id != expected_counterpart_type {
        return Err("linked object does not match configured link type".to_string());
    }

    persist_link_instance(
        state,
        claims,
        actor_id,
        &link_type,
        source_object_id,
        target_object_id,
        instruction.properties.clone(),
        "failed to create link from function response",
    )
    .await
}

fn derive_function_effects(
    response: &Value,
) -> Result<
    (
        Option<Value>,
        Option<Value>,
        Option<FunctionLinkInstruction>,
        bool,
    ),
    String,
> {
    let Some(object) = response.as_object() else {
        return Ok((Some(response.clone()), None, None, false));
    };

    let output = object
        .get("output")
        .filter(|value| !value.is_null())
        .cloned();
    let object_patch = object
        .get("object_patch")
        .filter(|value| !value.is_null())
        .cloned();
    let link = object
        .get("link")
        .filter(|value| !value.is_null())
        .cloned()
        .map(serde_json::from_value::<FunctionLinkInstruction>)
        .transpose()
        .map_err(|e| format!("invalid function link instruction: {e}"))?;
    let delete_object = object
        .get("delete_object")
        .and_then(Value::as_bool)
        .unwrap_or(false);

    if delete_object && (object_patch.is_some() || link.is_some()) {
        return Err(
            "function response cannot request delete_object together with object_patch or link"
                .to_string(),
        );
    }

    let result = if output.is_some() {
        output
    } else if object_patch.is_none() && link.is_none() && !delete_object {
        Some(response.clone())
    } else {
        None
    };

    Ok((result, object_patch, link, delete_object))
}

async fn validate_action_definition(
    state: &AppState,
    object_type_id: Uuid,
    operation_kind_raw: &str,
    input_schema: &[ActionInputField],
    form_schema: &ActionFormSchema,
    config: &Value,
    authorization_policy: &ActionAuthorizationPolicy,
) -> Result<ActionOperationKind, String> {
    if !ensure_object_type_exists(state, object_type_id)
        .await
        .map_err(|e| format!("failed to validate object type: {e}"))?
    {
        return Err("referenced object type does not exist".to_string());
    }

    ensure_input_schema(input_schema)?;
    validate_action_authorization_policy(authorization_policy)?;
    let operation_kind = parse_operation_kind(operation_kind_raw)?;
    let (operation_config, notification_side_effects) = split_action_config(config)?;
    let input_names = input_schema
        .iter()
        .map(|field| field.name.as_str())
        .collect::<HashSet<_>>();
    let effective_properties = action_repository::load_effective_properties(
        state.stores.definitions.as_ref(),
        object_type_id,
    )
    .await
    .map_err(|e| format!("failed to load property definitions: {e}"))?;
    let property_types = effective_properties
        .iter()
        .map(|property| (property.name.as_str(), property.property_type.as_str()))
        .collect::<HashMap<_, _>>();

    validate_action_form_schema(form_schema, input_schema, &property_types)?;
    validate_notification_side_effect_configs(
        &notification_side_effects,
        &input_names,
        &property_types,
    )?;
    // TASK G — Validate that webhook input mappings reference declared
    // action input fields. Mirrors Foundry's `OntologyMetadata:
    // ActionWebhookInputsDoNotHaveExpectedType` rejection at edit time.
    let webhook_envelope = parse_action_envelope(config)?;
    if let Some(envelope) = webhook_envelope.as_ref() {
        if envelope.batched_execution && operation_kind != ActionOperationKind::InvokeFunction {
            return Err("batched_execution is only valid on function-backed actions".to_string());
        }
        if let Some(writeback) = envelope.webhook_writeback.as_ref() {
            validate_webhook_call_config(writeback, &input_names, "webhook_writeback")?;
        }
        for (index, side_effect) in envelope.webhook_side_effects.iter().enumerate() {
            validate_webhook_call_config(
                side_effect,
                &input_names,
                &format!("webhook_side_effects[{index}]"),
            )?;
        }
    }

    match operation_kind {
        ActionOperationKind::UpdateObject => {
            let cfg: UpdateObjectActionConfig = serde_json::from_value(operation_config)
                .map_err(|e| format!("invalid update_object action config: {e}"))?;
            if cfg.property_mappings.is_empty() && cfg.static_patch.as_ref().is_none() {
                return Err(
                    "update_object action requires property_mappings or static_patch".to_string(),
                );
            }

            for mapping in cfg.property_mappings {
                if mapping.property_name.trim().is_empty() {
                    return Err("property_name is required for update_object mappings".to_string());
                }

                let property_type = property_types
                    .get(mapping.property_name.as_str())
                    .ok_or_else(|| {
                        format!(
                            "unknown property '{}' in update_object action config",
                            mapping.property_name
                        )
                    })?;

                match (&mapping.input_name, &mapping.value) {
                    (Some(input_name), None) => {
                        if !input_names.contains(input_name.as_str()) {
                            return Err(format!(
                                "unknown input field '{input_name}' in action config"
                            ));
                        }
                    }
                    (None, Some(value)) => {
                        validate_property_value(property_type, value)
                            .map_err(|error| format!("{}: {}", mapping.property_name, error))?;
                    }
                    _ => {
                        return Err(
                            "each update_object mapping needs either input_name or value"
                                .to_string(),
                        );
                    }
                }
            }

            if let Some(static_patch) = cfg.static_patch {
                let values = static_patch
                    .as_object()
                    .ok_or_else(|| "static_patch must be a JSON object".to_string())?;
                for (property_name, value) in values {
                    let property_type =
                        property_types.get(property_name.as_str()).ok_or_else(|| {
                            format!("unknown property '{property_name}' in static_patch")
                        })?;
                    validate_property_value(property_type, value)
                        .map_err(|error| format!("{}: {}", property_name, error))?;
                }
            }
        }
        ActionOperationKind::CreateLink => {
            let cfg: CreateLinkActionConfig = serde_json::from_value(operation_config)
                .map_err(|e| format!("invalid create_link action config: {e}"))?;
            if !input_names.contains(cfg.target_input_name.as_str()) {
                return Err(format!(
                    "target_input_name '{}' does not match any input schema field",
                    cfg.target_input_name
                ));
            }
            if let Some(properties_input_name) = cfg.properties_input_name.as_ref() {
                if !input_names.contains(properties_input_name.as_str()) {
                    return Err(format!(
                        "properties_input_name '{}' does not match any input schema field",
                        properties_input_name
                    ));
                }
            }
            if cfg.source_role != "source" && cfg.source_role != "target" {
                return Err("create_link source_role must be 'source' or 'target'".to_string());
            }
            let link_type = action_repository::load_link_type(
                state.stores.definitions.as_ref(),
                cfg.link_type_id,
            )
            .await
            .map_err(|e| format!("failed to validate link type: {e}"))?
            .ok_or_else(|| "referenced link type does not exist".to_string())?;
            let expected_type = if cfg.source_role == "source" {
                link_type.source_type_id
            } else {
                link_type.target_type_id
            };
            if expected_type != object_type_id {
                return Err(
                    "action object_type_id does not match configured link endpoint".to_string(),
                );
            }
        }
        ActionOperationKind::DeleteObject => {
            if !operation_config.is_null()
                && !operation_config
                    .as_object()
                    .map(|value| value.is_empty())
                    .unwrap_or(false)
            {
                return Err("delete_object actions do not accept config".to_string());
            }
        }
        ActionOperationKind::InvokeFunction => {
            if resolve_inline_function_config(state, &operation_config)
                .await?
                .is_none()
            {
                validate_http_invocation_config(&operation_config)?;
            }
        }
        ActionOperationKind::InvokeWebhook => {
            validate_http_invocation_config(&operation_config)?;
        }
        // TASK I — Interface-typed actions are validated at runtime once
        // `__object_type` / `__interface_ref` resolves to a concrete
        // object_type. Edit-time validation is currently lenient: we only
        // require the auto-generated parameter to exist in the input schema.
        ActionOperationKind::CreateInterface => {
            ensure_input_field_exists(input_schema, "__object_type", "create_interface")?;
        }
        ActionOperationKind::ModifyInterface
        | ActionOperationKind::DeleteInterface
        | ActionOperationKind::CreateInterfaceLink
        | ActionOperationKind::DeleteInterfaceLink => {
            ensure_input_field_exists(
                input_schema,
                "__interface_ref",
                operation_kind.to_string().as_str(),
            )?;
        }
    }

    Ok(operation_kind)
}

/// TASK I — Asserts the auto-generated parameter is declared on the action.
/// Used for interface-typed operations where the parameter resolves to the
/// concrete `object_type_id` at runtime.
fn ensure_input_field_exists(
    input_schema: &[ActionInputField],
    name: &str,
    operation: &str,
) -> Result<(), String> {
    if input_schema.iter().any(|field| field.name == name) {
        Ok(())
    } else {
        Err(format!(
            "{operation} action requires an auto-generated input field '{name}'"
        ))
    }
}

async fn load_and_authorize_target(
    state: &AppState,
    claims: &Claims,
    target_object_id: Uuid,
    object_type_id: Uuid,
) -> Result<ObjectInstance, Vec<String>> {
    let target = load_object_instance(state, claims, target_object_id, ReadConsistency::Strong)
        .await
        .map_err(|e| vec![format!("failed to load target object: {e}")])?
        .ok_or_else(|| vec!["target object was not found".to_string()])?;
    ensure_object_type_match(&target, object_type_id).map_err(|e| vec![e])?;
    ensure_object_access(claims, &target).map_err(|e| vec![e])?;
    Ok(target)
}

async fn plan_action(
    state: &AppState,
    claims: &Claims,
    action: &ActionType,
    request: &ValidateActionRequest,
) -> Result<ActionPlan, Vec<String>> {
    let materialization_target = match request.target_object_id {
        Some(target_object_id) => Some(
            load_and_authorize_target(state, claims, target_object_id, action.object_type_id)
                .await?,
        ),
        None => None,
    };
    let parameters = materialize_parameters(
        &action.input_schema,
        &request.parameters,
        &action.form_schema,
        materialization_target.as_ref(),
    )?;
    let operation_kind = match parse_operation_kind(&action.operation_kind) {
        Ok(kind) => kind,
        Err(error) => return Err(vec![error]),
    };
    let (operation_config, notification_side_effects) = match split_action_config(&action.config) {
        Ok(parts) => parts,
        Err(error) => return Err(vec![error]),
    };

    match operation_kind {
        ActionOperationKind::UpdateObject => {
            let target_object_id = request.target_object_id.ok_or_else(|| {
                vec!["target_object_id is required for update_object actions".to_string()]
            })?;
            let target =
                load_and_authorize_target(state, claims, target_object_id, action.object_type_id)
                    .await?;
            ensure_action_target_permission(action, Some(&target)).map_err(|e| vec![e])?;

            let cfg: UpdateObjectActionConfig = serde_json::from_value(operation_config)
                .map_err(|e| vec![format!("invalid action config: {e}")])?;
            let property_types = action_repository::load_effective_properties(
                state.stores.definitions.as_ref(),
                action.object_type_id,
            )
            .await
            .map_err(|e| vec![format!("failed to load property definitions: {e}")])?
            .into_iter()
            .map(|property| (property.name, property.property_type))
            .collect::<HashMap<_, _>>();

            let mut patch = Map::new();
            for mapping in cfg.property_mappings {
                let property_type = property_types
                    .get(mapping.property_name.as_str())
                    .ok_or_else(|| {
                        vec![format!(
                            "unknown property '{}' in update_object action config",
                            mapping.property_name
                        )]
                    })?;
                let value = if let Some(input_name) = mapping.input_name {
                    parameters.get(&input_name).cloned().ok_or_else(|| {
                        vec![format!("missing input '{input_name}' for property mapping")]
                    })?
                } else {
                    mapping.value.unwrap_or(Value::Null)
                };

                validate_property_value(property_type, &value)
                    .map_err(|e| vec![format!("{}: {}", mapping.property_name, e)])?;
                patch.insert(mapping.property_name, value);
            }

            if let Some(static_patch) = cfg.static_patch {
                if let Some(values) = static_patch.as_object() {
                    for (property_name, value) in values {
                        let property_type =
                            property_types.get(property_name.as_str()).ok_or_else(|| {
                                vec![format!(
                                    "unknown property '{property_name}' in static_patch"
                                )]
                            })?;
                        validate_property_value(property_type, value)
                            .map_err(|e| vec![format!("{}: {}", property_name, e)])?;
                        patch.insert(property_name.to_string(), value.clone());
                    }
                }
            }

            validate_notification_resolution(
                &notification_side_effects,
                &parameters,
                Some(&target),
                claims.sub,
            )
            .map_err(|error| vec![error])?;

            Ok(ActionPlan::UpdateObject { target, patch })
        }
        ActionOperationKind::CreateLink => {
            let target_object_id = request.target_object_id.ok_or_else(|| {
                vec!["target_object_id is required for create_link actions".to_string()]
            })?;
            let target =
                load_and_authorize_target(state, claims, target_object_id, action.object_type_id)
                    .await?;
            ensure_action_target_permission(action, Some(&target)).map_err(|e| vec![e])?;

            let cfg: CreateLinkActionConfig = serde_json::from_value(operation_config)
                .map_err(|e| vec![format!("invalid action config: {e}")])?;
            let counterpart_id =
                resolve_uuid_parameter(&parameters, &cfg.target_input_name).map_err(|e| vec![e])?;
            let counterpart =
                load_object_instance(state, claims, counterpart_id, ReadConsistency::Strong)
                    .await
                    .map_err(|e| vec![format!("failed to load linked object: {e}")])?
                    .ok_or_else(|| vec!["linked object was not found".to_string()])?;
            ensure_object_access(claims, &counterpart).map_err(|e| vec![e])?;
            let link_type = action_repository::load_link_type(
                state.stores.definitions.as_ref(),
                cfg.link_type_id,
            )
            .await
            .map_err(|e| vec![format!("failed to load link type: {e}")])?
            .ok_or_else(|| vec!["configured link type was not found".to_string()])?;

            let (source_object_id, target_link_object_id, expected_counterpart_type) =
                if cfg.source_role == "source" {
                    (target.id, counterpart.id, link_type.target_type_id)
                } else {
                    (counterpart.id, target.id, link_type.source_type_id)
                };

            if counterpart.object_type_id != expected_counterpart_type {
                return Err(vec![
                    "linked object does not match configured link type".to_string(),
                ]);
            }

            let properties = cfg
                .properties_input_name
                .and_then(|field_name| parameters.get(&field_name).cloned());

            validate_notification_resolution(
                &notification_side_effects,
                &parameters,
                Some(&target),
                claims.sub,
            )
            .map_err(|error| vec![error])?;

            Ok(ActionPlan::CreateLink {
                target,
                counterpart,
                link_type,
                properties,
                source_object_id,
                target_object_id: target_link_object_id,
            })
        }
        ActionOperationKind::DeleteObject => {
            let target_object_id = request.target_object_id.ok_or_else(|| {
                vec!["target_object_id is required for delete_object actions".to_string()]
            })?;
            let target =
                load_and_authorize_target(state, claims, target_object_id, action.object_type_id)
                    .await?;
            ensure_action_target_permission(action, Some(&target)).map_err(|e| vec![e])?;

            validate_notification_resolution(
                &notification_side_effects,
                &parameters,
                Some(&target),
                claims.sub,
            )
            .map_err(|error| vec![error])?;

            Ok(ActionPlan::DeleteObject { target })
        }
        ActionOperationKind::InvokeFunction => {
            let target = match request.target_object_id {
                Some(target_object_id) => Some(
                    load_and_authorize_target(
                        state,
                        claims,
                        target_object_id,
                        action.object_type_id,
                    )
                    .await?,
                ),
                None => None,
            };
            ensure_action_target_permission(action, target.as_ref()).map_err(|e| vec![e])?;
            let payload = build_http_payload(action, target.as_ref(), &parameters);
            let invocation = match resolve_inline_function_config(state, &operation_config).await {
                Ok(Some(config)) => FunctionInvocation::Inline(config),
                Ok(None) => FunctionInvocation::Http(
                    validate_http_invocation_config(&operation_config).map_err(|e| vec![e])?,
                ),
                Err(error) => return Err(vec![error]),
            };

            validate_notification_resolution(
                &notification_side_effects,
                &parameters,
                target.as_ref(),
                claims.sub,
            )
            .map_err(|error| vec![error])?;

            Ok(ActionPlan::InvokeFunction {
                target,
                invocation,
                payload,
                parameters,
            })
        }
        ActionOperationKind::InvokeWebhook => {
            let target = match request.target_object_id {
                Some(target_object_id) => Some(
                    load_and_authorize_target(
                        state,
                        claims,
                        target_object_id,
                        action.object_type_id,
                    )
                    .await?,
                ),
                None => None,
            };
            ensure_action_target_permission(action, target.as_ref()).map_err(|e| vec![e])?;
            let payload = build_http_payload(action, target.as_ref(), &parameters);
            let invocation =
                validate_http_invocation_config(&operation_config).map_err(|e| vec![e])?;

            validate_notification_resolution(
                &notification_side_effects,
                &parameters,
                target.as_ref(),
                claims.sub,
            )
            .map_err(|error| vec![error])?;

            Ok(ActionPlan::InvokeWebhook {
                target,
                invocation,
                payload,
            })
        }
        // TASK I — Interface-typed operations resolve to a concrete object
        // type at runtime. The current MVP rejects execution with a clear
        // error so callers can wire the resolution flow incrementally; the
        // edit-time validation already checks that the auto-generated
        // parameter is declared.
        ActionOperationKind::CreateInterface
        | ActionOperationKind::ModifyInterface
        | ActionOperationKind::DeleteInterface
        | ActionOperationKind::CreateInterfaceLink
        | ActionOperationKind::DeleteInterfaceLink => Err(vec![format!(
            "interface-typed action '{operation_kind}' is not yet executable; \
             resolution from interface_id to concrete object_type pending. \
             Action logs are not yet supported for interfaces."
        )]),
    }
}

fn plan_preview(plan: &ActionPlan) -> Value {
    match plan {
        ActionPlan::UpdateObject { target, patch } => json!({
            "kind": "update_object",
            "target_object_id": target.id,
            "patch": patch,
        }),
        ActionPlan::CreateLink {
            target,
            counterpart,
            link_type,
            properties,
            source_object_id,
            target_object_id,
        } => json!({
            "kind": "create_link",
            "target_object_id": target.id,
            "counterpart_object_id": counterpart.id,
            "link_type_id": link_type.id,
            "source_object_id": source_object_id,
            "linked_object_id": target_object_id,
            "properties": properties,
        }),
        ActionPlan::DeleteObject { target } => json!({
            "kind": "delete_object",
            "target_object_id": target.id,
        }),
        ActionPlan::InvokeFunction {
            target,
            invocation,
            payload,
            ..
        } => match invocation {
            FunctionInvocation::Http(invocation) => json!({
                "kind": "invoke_function",
                "runtime": "http",
                "target_object_id": target.as_ref().map(|object| object.id),
                "request": {
                    "url": &invocation.url,
                    "method": &invocation.method,
                    "headers": &invocation.headers,
                    "payload": payload,
                },
            }),
            FunctionInvocation::Inline(invocation) => json!({
                "kind": "invoke_function",
                "runtime": invocation.runtime_name(),
                "target_object_id": target.as_ref().map(|object| object.id),
                "request": {
                    "payload": payload,
                },
                "source_length": invocation.source_len(),
                "capabilities": invocation.capabilities,
                "function_package": invocation.package,
            }),
        },
        ActionPlan::InvokeWebhook {
            target,
            invocation,
            payload,
        } => json!({
            "kind": "invoke_webhook",
            "target_object_id": target.as_ref().map(|object| object.id),
            "request": {
                "url": &invocation.url,
                "method": &invocation.method,
                "headers": &invocation.headers,
                "payload": payload,
            },
        }),
    }
}

fn target_snapshot_from_plan(plan: &ActionPlan) -> Option<ObjectInstance> {
    match plan {
        ActionPlan::UpdateObject { target, .. }
        | ActionPlan::CreateLink { target, .. }
        | ActionPlan::DeleteObject { target }
        | ActionPlan::InvokeFunction {
            target: Some(target),
            ..
        }
        | ActionPlan::InvokeWebhook {
            target: Some(target),
            ..
        } => Some(target.clone()),
        _ => None,
    }
}

async fn simulate_target_after_preview(
    state: &AppState,
    target: &ObjectInstance,
    preview: &Value,
) -> Result<Option<Value>, String> {
    if preview
        .get("kind")
        .and_then(Value::as_str)
        .is_some_and(|kind| kind == "delete_object")
    {
        return Ok(None);
    }

    let mut merged = target.properties.as_object().cloned().unwrap_or_default();
    if let Some(patch) = preview.get("patch").and_then(Value::as_object) {
        for (key, value) in patch {
            merged.insert(key.clone(), value.clone());
        }
    }

    let definitions = action_repository::load_effective_properties(
        state.stores.definitions.as_ref(),
        target.object_type_id,
    )
    .await
    .map_err(|error| format!("failed to load property definitions: {error}"))?;
    let normalized = validate_object_properties(&definitions, &Value::Object(merged))
        .map_err(|error| format!("invalid simulated action branch: {error}"))?;

    let mut simulated = target.clone();
    simulated.properties = normalized;
    simulated.updated_at = chrono::Utc::now();
    Ok(Some(json!(simulated)))
}

fn issue_service_token(state: &AppState, claims: &Claims) -> Result<String, String> {
    let service_claims = build_access_claims(
        &state.jwt_config,
        Uuid::now_v7(),
        "ontology-service@internal.openfoundry",
        "ontology-service",
        vec!["admin".to_string()],
        vec!["*:*".to_string()],
        claims.org_id,
        json!({
            "service": "ontology-service",
            "classification_clearance": "pii",
            "impersonated_actor_id": claims.sub,
        }),
        vec!["service".to_string()],
    );
    let token = encode_token(&state.jwt_config, &service_claims)
        .map_err(|error| format!("failed to issue service token for audit: {error}"))?;
    Ok(format!("Bearer {token}"))
}

fn classification_for_target(target: Option<&ObjectInstance>) -> &'static str {
    match target.map(|object| object.marking.as_str()) {
        Some("confidential") => "confidential",
        Some("pii") => "pii",
        _ => "public",
    }
}

async fn emit_action_audit_event(
    state: &AppState,
    claims: &Claims,
    action: &ActionType,
    target: Option<&ObjectInstance>,
    target_object_id: Option<Uuid>,
    status: &str,
    severity: &str,
    message: Option<&str>,
    justification: Option<&str>,
    parameters: &Value,
    preview: Option<&Value>,
    result: Option<&Value>,
) -> Result<(), String> {
    let token = issue_service_token(state, claims)?;
    let url = format!(
        "{}/api/v1/audit/events",
        state.audit_service_url.trim_end_matches('/')
    );
    let resource_id = target_object_id.unwrap_or(action.id).to_string();
    let metadata = json!({
        "action_id": action.id,
        "action_name": &action.name,
        "operation_kind": &action.operation_kind,
        "object_type_id": action.object_type_id,
        "permission_key": &action.permission_key,
        "authorization_policy": &action.authorization_policy,
        "target_object_id": target_object_id,
        "justification": justification,
        "parameters": parameters,
        "preview": preview,
        "result": result,
        "message": message,
        "actor_id": claims.sub,
        "actor_roles": &claims.roles,
        "organization_id": claims.org_id,
    });

    let response = state
        .http_client
        .post(url)
        .header("authorization", token)
        .json(&json!({
            "source_service": "ontology-service",
            "channel": "api",
            "actor": &claims.email,
            "action": "ontology.action.execute",
            "resource_type": if target_object_id.is_some() {
                "ontology_object"
            } else {
                "ontology_action"
            },
            "resource_id": resource_id,
            "status": status,
            "severity": severity,
            "classification": classification_for_target(target),
            "subject_id": claims.sub.to_string(),
            "metadata": metadata,
            "labels": ["ontology", "action", status, action.operation_kind.as_str()],
        }))
        .send()
        .await
        .map_err(|error| format!("failed to send audit event: {error}"))?;

    if response.status().is_success() {
        Ok(())
    } else {
        let status = response.status();
        let body = response.text().await.unwrap_or_default();
        Err(format!("audit service returned {status}: {body}"))
    }
}

async fn send_notification_request(
    state: &AppState,
    request: &InternalSendNotificationRequest,
) -> Result<(), String> {
    let url = format!(
        "{}/internal/notifications",
        state.notification_service_url.trim_end_matches('/')
    );

    let response = state
        .http_client
        .post(url)
        .json(request)
        .send()
        .await
        .map_err(|error| format!("failed to send action notification: {error}"))?;

    if response.status().is_success() {
        Ok(())
    } else {
        let status = response.status();
        let body = response.text().await.unwrap_or_default();
        Err(format!("notification service returned {status}: {body}"))
    }
}

async fn emit_action_notifications(
    state: &AppState,
    claims: &Claims,
    action: &ActionType,
    target: Option<&ObjectInstance>,
    parameters: &Value,
    justification: Option<&str>,
    executed: &ExecutedAction,
) -> Result<(), String> {
    let (_, notification_configs) = split_action_config(&action.config)?;
    if notification_configs.is_empty() {
        return Ok(());
    }

    let parameters_map = materialize_parameters(
        &action.input_schema,
        parameters,
        &action.form_schema,
        target,
    )
    .map_err(|errors| errors.join("; "))?;

    let context = json!({
        "action": {
            "id": action.id,
            "name": &action.name,
            "display_name": &action.display_name,
            "description": &action.description,
            "operation_kind": &action.operation_kind,
            "object_type_id": action.object_type_id,
        },
        "actor": {
            "id": claims.sub,
            "email": &claims.email,
            "roles": &claims.roles,
            "organization_id": claims.org_id,
        },
        "target": target,
        "parameters": &parameters_map,
        "justification": justification,
        "preview": &executed.preview,
        "execution": {
            "target_object_id": executed.target_object_id,
            "deleted": executed.deleted,
        },
        "object": &executed.object,
        "link": &executed.link,
        "result": &executed.result,
    });

    for config in notification_configs {
        let (recipients, broadcast) =
            resolve_notification_recipients(&config, &parameters_map, target, claims.sub)?;
        let title = render_template(&config.title, &context);
        let body = render_template(&config.body, &context);
        let metadata = build_notification_metadata(&config, &context, action, executed);

        let channels = config.channels.clone();
        let severity = config.severity.clone();
        let category = config.category.clone();

        if broadcast {
            send_notification_request(
                state,
                &InternalSendNotificationRequest {
                    user_id: None,
                    title: title.clone(),
                    body: body.clone(),
                    severity: severity.clone(),
                    category: category.clone(),
                    channels: channels.clone(),
                    metadata: Some(metadata.clone()),
                },
            )
            .await?;
        }

        for user_id in recipients {
            send_notification_request(
                state,
                &InternalSendNotificationRequest {
                    user_id: Some(user_id),
                    title: title.clone(),
                    body: body.clone(),
                    severity: severity.clone(),
                    category: category.clone(),
                    channels: channels.clone(),
                    metadata: Some(metadata.clone()),
                },
            )
            .await?;
        }
    }

    Ok(())
}

async fn execute_plan(
    state: &AppState,
    claims: &Claims,
    action: &ActionType,
    justification: Option<&str>,
    plan: ActionPlan,
) -> Result<ExecutedAction, String> {
    let preview = plan_preview(&plan);

    match plan {
        ActionPlan::UpdateObject { target, patch } => {
            let updated = apply_object_patch(state, claims, &target, &Value::Object(patch)).await?;
            Ok(ExecutedAction {
                target_object_id: Some(target.id),
                deleted: false,
                preview,
                object: Some(json!(updated)),
                link: None,
                result: None,
            })
        }
        ActionPlan::CreateLink {
            target,
            link_type,
            properties,
            source_object_id,
            target_object_id,
            ..
        } => {
            let link = persist_link_instance(
                state,
                claims,
                claims.sub,
                &link_type,
                source_object_id,
                target_object_id,
                properties,
                "failed to execute create_link action",
            )
            .await?;

            Ok(ExecutedAction {
                target_object_id: Some(target.id),
                deleted: false,
                preview,
                object: None,
                link: Some(json!(link)),
                result: None,
            })
        }
        ActionPlan::DeleteObject { target } => {
            delete_object_via_store(
                state,
                claims,
                target.id,
                "failed to execute delete_object action via ObjectStore",
            )
            .await?;

            Ok(ExecutedAction {
                target_object_id: Some(target.id),
                deleted: true,
                preview,
                object: None,
                link: None,
                result: None,
            })
        }
        ActionPlan::InvokeWebhook {
            target,
            invocation,
            payload,
        } => {
            let result = invoke_http_action(state, &invocation, &payload).await?;
            Ok(ExecutedAction {
                target_object_id: target.as_ref().map(|object| object.id),
                deleted: false,
                preview,
                object: None,
                link: None,
                result: Some(result),
            })
        }
        ActionPlan::InvokeFunction {
            target,
            invocation,
            payload,
            parameters,
        } => match &invocation {
            FunctionInvocation::Http(invocation) => {
                let response = invoke_http_action(state, invocation, &payload).await?;

                let (result, object_patch, link_instruction, delete_object) =
                    derive_function_effects(&response)
                        .map_err(|e| format!("invalid function response: {e}"))?;

                let Some(target_object) = target.as_ref() else {
                    if object_patch.is_some() || link_instruction.is_some() || delete_object {
                        return Err(
                                "function response requested ontology mutations but target_object_id was not provided"
                                    .to_string(),
                            );
                    }

                    return Ok(ExecutedAction {
                        target_object_id: None,
                        deleted: false,
                        preview,
                        object: None,
                        link: None,
                        result: result.or(Some(response)),
                    });
                };

                let object = match object_patch {
                    Some(patch) => Some(json!(
                        apply_object_patch(state, claims, target_object, &patch).await?
                    )),
                    None => None,
                };

                let link = match link_instruction {
                    Some(instruction) => Some(json!(
                        create_link_from_instruction(
                            state,
                            claims,
                            claims.sub,
                            target_object,
                            &instruction
                        )
                        .await?
                    )),
                    None => None,
                };

                let deleted = if delete_object {
                    delete_object_via_store(
                        state,
                        claims,
                        target_object.id,
                        "failed to delete object from function response via ObjectStore",
                    )
                    .await?;
                    true
                } else {
                    false
                };

                Ok(ExecutedAction {
                    target_object_id: Some(target_object.id),
                    deleted,
                    preview,
                    object,
                    link,
                    result: result.or(Some(response)),
                })
            }
            FunctionInvocation::Inline(config) => {
                let started_at = Utc::now();
                let timer = Instant::now();
                let package = config.package.clone();
                let outcome: Result<ExecutedAction, String> = async {
                        let response = execute_inline_function(
                            state,
                            claims,
                            action,
                            target.as_ref(),
                            &parameters,
                            config,
                            justification,
                        )
                        .await?;

                        let (result, object_patch, link_instruction, delete_object) =
                            derive_function_effects(&response)
                                .map_err(|e| format!("invalid function response: {e}"))?;

                        let Some(target_object) = target.as_ref() else {
                            if object_patch.is_some()
                                || link_instruction.is_some()
                                || delete_object
                            {
                                return Err(
                                    "function response requested ontology mutations but target_object_id was not provided"
                                        .to_string(),
                                );
                            }

                            return Ok(ExecutedAction {
                                target_object_id: None,
                                deleted: false,
                                preview: preview.clone(),
                                object: None,
                                link: None,
                                result: result.or(Some(response)),
                            });
                        };

                        let object = match object_patch {
                            Some(patch) => Some(json!(
                                apply_object_patch(state, claims, target_object, &patch).await?
                            )),
                            None => None,
                        };

                        let link = match link_instruction {
                            Some(instruction) => Some(json!(
                                create_link_from_instruction(
                                    state,
                                    claims,
                                    claims.sub,
                                    target_object,
                                    &instruction
                                )
                                .await?
                            )),
                            None => None,
                        };

                        let deleted = if delete_object {
                            delete_object_via_store(
                                state,
                                claims,
                                target_object.id,
                                "failed to delete object from function response via ObjectStore",
                            )
                            .await?;
                            true
                        } else {
                            false
                        };

                        Ok(ExecutedAction {
                            target_object_id: Some(target_object.id),
                            deleted,
                            preview: preview.clone(),
                            object,
                            link,
                            result: result.or(Some(response)),
                        })
                    }
                    .await;

                let completed_at = Utc::now();
                let duration_ms = timer.elapsed().as_millis() as i64;

                if let Some(package) = package.as_ref() {
                    let run_context = FunctionPackageRunContext {
                        invocation_kind: "action",
                        action_id: Some(action.id),
                        action_name: Some(action.name.as_str()),
                        object_type_id: Some(action.object_type_id),
                        target_object_id: target.as_ref().map(|object| object.id),
                        actor_id: claims.sub,
                    };
                    let status = if outcome.is_ok() {
                        "success"
                    } else {
                        "failure"
                    };
                    let error_message = outcome.as_ref().err().map(String::as_str);

                    if let Err(error) = record_function_package_run(
                        state,
                        package,
                        &run_context,
                        started_at,
                        completed_at,
                        duration_ms,
                        status,
                        error_message,
                    )
                    .await
                    {
                        tracing::warn!(action_id = %action.id, function_package_id = %package.id, %error, "failed to record function-backed action run");
                    }
                }

                outcome
            }
        },
    }
}

fn log_audit_failure(action_id: Uuid, error: &str) {
    tracing::warn!(%action_id, %error, "failed to emit ontology action audit event");
}

fn log_notification_failure(action_id: Uuid, error: &str) {
    tracing::warn!(%action_id, %error, "failed to emit ontology action notification");
}

fn deterministic_action_event_id(parts: &[String]) -> String {
    Uuid::new_v5(
        &Uuid::NAMESPACE_OID,
        format!("ontology/action/{}", parts.join("/")).as_bytes(),
    )
    .to_string()
}

pub(crate) async fn preview_action_for_simulation(
    state: &AppState,
    claims: &Claims,
    action_id: Uuid,
    target_object_id: Option<Uuid>,
    parameters: Value,
) -> Result<Value, String> {
    let row = load_action_row(state, action_id)
        .await
        .map_err(|error| format!("failed to load action type: {error}"))?
        .ok_or_else(|| "action type was not found".to_string())?;
    let action = ActionType::try_from(row)
        .map_err(|error| format!("failed to decode action type: {error}"))?;

    ensure_action_actor_permission(claims, &action)?;
    let plan = plan_action(
        state,
        claims,
        &action,
        &ValidateActionRequest {
            target_object_id,
            parameters,
        },
    )
    .await
    .map_err(|errors| errors.join("; "))?;

    Ok(plan_preview(&plan))
}

pub async fn create_action_type(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Json(body): Json<CreateActionTypeRequest>,
) -> impl IntoResponse {
    if body.name.trim().is_empty() {
        return invalid_action("action type name is required");
    }

    let display_name = body.display_name.unwrap_or_else(|| body.name.clone());
    let description = body.description.unwrap_or_default();
    let input_schema = body.input_schema.unwrap_or_default();
    let form_schema = body.form_schema.unwrap_or_default();
    let config = body.config.unwrap_or(Value::Null);
    let authorization_policy = body.authorization_policy.unwrap_or_default();

    if let Err(error) = validate_action_definition(
        &state,
        body.object_type_id,
        &body.operation_kind,
        &input_schema,
        &form_schema,
        &config,
        &authorization_policy,
    )
    .await
    {
        return invalid_action(error);
    }

    let now = Utc::now();
    let action_type = ActionType {
        id: Uuid::now_v7(),
        name: body.name,
        display_name,
        description,
        object_type_id: body.object_type_id,
        operation_kind: body.operation_kind,
        input_schema,
        form_schema,
        config,
        confirmation_required: body.confirmation_required.unwrap_or(false),
        permission_key: body.permission_key,
        authorization_policy,
        owner_id: claims.sub,
        created_at: now,
        updated_at: now,
    };

    match action_repository::put_action(state.stores.definitions.as_ref(), action_type.clone())
        .await
    {
        Ok(_) => (StatusCode::CREATED, Json(json!(action_type))).into_response(),
        Err(e) => db_error(format!("create action type failed: {e}")),
    }
}

pub async fn list_action_types(
    _user: AuthUser,
    State(state): State<AppState>,
    Query(params): Query<ListActionTypesQuery>,
) -> impl IntoResponse {
    let page = params.page.unwrap_or(1).max(1);
    let per_page = params.per_page.unwrap_or(20).clamp(1, 100);
    let offset = (page - 1) * per_page;
    let search = params.search.clone();
    let total = action_repository::count_action_rows(
        state.stores.definitions.as_ref(),
        params.object_type_id,
        search.clone(),
    )
    .await
    .unwrap_or(0) as i64;

    let rows = action_repository::list_action_rows(
        state.stores.definitions.as_ref(),
        action_repository::ActionTypeListQuery {
            object_type_id: params.object_type_id,
            search,
            page: Page {
                size: per_page as u32,
                token: Some(offset.to_string()),
            },
        },
    )
    .await
    .map(|page| page.items)
    .unwrap_or_default();

    let mut data = Vec::new();
    for row in rows {
        match ActionType::try_from(row) {
            Ok(action_type) => data.push(action_type),
            Err(e) => return db_error(format!("failed to decode action type row: {e}")),
        }
    }

    Json(ListActionTypesResponse {
        data,
        total,
        page,
        per_page,
    })
    .into_response()
}

pub async fn get_action_type(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match load_action_row(&state, id).await {
        Ok(Some(row)) => match ActionType::try_from(row) {
            Ok(action_type) => Json(json!(action_type)).into_response(),
            Err(e) => db_error(format!("failed to decode action type: {e}")),
        },
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(e) => db_error(format!("failed to load action type: {e}")),
    }
}

#[derive(Debug, Default, Deserialize)]
pub struct ListApplicableActionsQuery {
    /// `single` lists actions whose input schema does NOT contain an
    /// `object_reference_list`/list-typed object reference (i.e. invokable
    /// from a single object selection). `bulk` lists the inverse — actions
    /// that REQUIRE a multi-object selection. When omitted, both kinds are
    /// returned. Mirrors `Use actions in the platform.md`.
    pub selection_kind: Option<String>,
}

/// TASK N — `GET /api/v1/ontology/types/{type_id}/applicable-actions`.
/// Returns action types attached to the object type, optionally filtered by
/// the selection kind so UI button groups can render the correct set
/// depending on whether the user has selected a single object or a bulk
/// selection.
pub async fn list_applicable_actions(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(type_id): Path<Uuid>,
    Query(query): Query<ListApplicableActionsQuery>,
) -> impl IntoResponse {
    let rows = action_repository::list_action_rows(
        state.stores.definitions.as_ref(),
        action_repository::ActionTypeListQuery {
            object_type_id: Some(type_id),
            search: None,
            page: Page {
                size: 500,
                token: None,
            },
        },
    )
    .await
    .map(|page| page.items)
    .unwrap_or_default();

    let selection_kind = query.selection_kind.as_deref().map(str::to_lowercase);
    let mut data = Vec::new();
    for row in rows {
        match ActionType::try_from(row) {
            Ok(action) => {
                let kind = action_selection_kind(&action);
                let include = match selection_kind.as_deref() {
                    Some("single") => kind == "single",
                    Some("bulk") => kind == "bulk",
                    _ => true,
                };
                if include {
                    data.push(json!({
                        "action_type": action,
                        "selection_kind": kind,
                    }));
                }
            }
            Err(error) => {
                return db_error(format!("failed to decode action type: {error}"));
            }
        }
    }

    Json(json!({ "data": data, "total": data.len() })).into_response()
}

/// TASK N — Classify an action's selection requirement by inspecting its
/// input schema. Any list-shaped object reference parameter forces a bulk
/// selection (mirrors Foundry's `object_reference_list` rule).
fn action_selection_kind(action: &ActionType) -> &'static str {
    let needs_bulk = action.input_schema.iter().any(|field| {
        matches!(
            field.property_type.as_str(),
            "object_reference_list" | "object_set"
        )
    });
    if needs_bulk { "bulk" } else { "single" }
}

pub async fn update_action_type(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<UpdateActionTypeRequest>,
) -> impl IntoResponse {
    let Some(existing_row) = (match load_action_row(&state, id).await {
        Ok(row) => row,
        Err(e) => return db_error(format!("failed to load action type: {e}")),
    }) else {
        return StatusCode::NOT_FOUND.into_response();
    };

    let existing = match ActionType::try_from(existing_row.clone()) {
        Ok(action_type) => action_type,
        Err(e) => return db_error(format!("failed to decode action type: {e}")),
    };

    let operation_kind = body
        .operation_kind
        .unwrap_or(existing.operation_kind.clone());
    let input_schema = body.input_schema.unwrap_or(existing.input_schema.clone());
    let form_schema = body.form_schema.unwrap_or(existing.form_schema.clone());
    let config = body.config.unwrap_or(existing.config.clone());
    let authorization_policy = body
        .authorization_policy
        .unwrap_or(existing.authorization_policy.clone());

    if let Err(error) = validate_action_definition(
        &state,
        existing.object_type_id,
        &operation_kind,
        &input_schema,
        &form_schema,
        &config,
        &authorization_policy,
    )
    .await
    {
        return invalid_action(error);
    }

    // TASK L — When this action is referenced by any property's
    // `inline_edit_config`, the new envelope must still meet the inline-edit
    // requirements (no side-effect webhooks, no notifications, single
    // object type, update_object operation).
    if let Err(error) =
        ensure_inline_edit_requirements_for_action(&state, id, &operation_kind, &config).await
    {
        return invalid_action(error);
    }
    let permission_key = body.permission_key.or(existing.permission_key);
    let updated = ActionType {
        id: existing.id,
        name: existing.name,
        display_name: body.display_name.unwrap_or(existing.display_name),
        description: body.description.unwrap_or(existing.description),
        object_type_id: existing.object_type_id,
        operation_kind,
        input_schema,
        form_schema,
        config,
        confirmation_required: body
            .confirmation_required
            .unwrap_or(existing.confirmation_required),
        permission_key,
        authorization_policy,
        owner_id: existing.owner_id,
        created_at: existing.created_at,
        updated_at: Utc::now(),
    };

    match action_repository::put_action(state.stores.definitions.as_ref(), updated.clone()).await {
        Ok(_) => Json(json!(updated)).into_response(),
        Err(e) => db_error(format!("failed to update action type: {e}")),
    }
}

pub async fn delete_action_type(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match action_repository::delete_action(state.stores.definitions.as_ref(), id).await {
        Ok(true) => StatusCode::NO_CONTENT.into_response(),
        Ok(false) => StatusCode::NOT_FOUND.into_response(),
        Err(e) => db_error(format!("failed to delete action type: {e}")),
    }
}

pub async fn validate_action(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<ValidateActionRequest>,
) -> impl IntoResponse {
    let Some(row) = (match load_action_row(&state, id).await {
        Ok(row) => row,
        Err(e) => return db_error(format!("failed to load action type: {e}")),
    }) else {
        return StatusCode::NOT_FOUND.into_response();
    };

    let action = match ActionType::try_from(row) {
        Ok(action_type) => action_type,
        Err(e) => return db_error(format!("failed to decode action type: {e}")),
    };

    if let Err(error) = ensure_action_actor_permission(&claims, &action) {
        return forbidden(error);
    }

    match plan_action(&state, &claims, &action, &body).await {
        Ok(plan) => Json(ValidateActionResponse {
            valid: true,
            errors: vec![],
            preview: plan_preview(&plan),
        })
        .into_response(),
        Err(errors) => {
            if all_forbidden(&errors) {
                forbidden(errors.join("; "))
            } else {
                Json(ValidateActionResponse {
                    valid: false,
                    errors,
                    preview: Value::Null,
                })
                .into_response()
            }
        }
    }
}

async fn execute_loaded_action(
    state: &AppState,
    claims: &Claims,
    action: ActionType,
    body: ExecuteActionRequest,
) -> Response {
    // TASK F — instrument every execution with Prometheus counters and a
    // best-effort Cassandra action-log row so the JSON metrics endpoint can
    // aggregate latency/failure counts without scraping Prometheus.
    let started_at = std::time::Instant::now();
    let action_id_for_metric = action.id;
    let action_id_label = action.id.to_string();
    let metrics_handle = crate::metrics::action_metrics();

    // TASK G — Resolve writeback / side-effect webhook configs once. The
    // writeback fires synchronously before `plan_action`; side effects fire
    // after a successful execution.
    let (webhook_writeback_cfg, webhook_side_effect_cfgs) =
        match split_webhook_configs(&action.config) {
            Ok(parts) => parts,
            Err(error) => return invalid_action(error),
        };

    let mut body = body;

    if let Err(error) = ensure_action_actor_permission(claims, &action) {
        if let Err(audit_error) = emit_action_audit_event(
            state,
            claims,
            &action,
            None,
            body.target_object_id,
            "denied",
            "medium",
            Some(&error),
            body.justification.as_deref(),
            &body.parameters,
            None,
            None,
        )
        .await
        {
            log_audit_failure(action.id, &audit_error);
        }
        record_action_failure_metric(
            state,
            claims,
            action.id,
            body.target_object_id,
            &body.parameters,
            crate::metrics::FailureType::Authentication,
            started_at,
            metrics_handle,
            &action_id_label,
        )
        .await;
        return forbidden(error);
    }

    if let Err(error) = ensure_confirmation_justification(&action, body.justification.as_deref()) {
        if let Err(audit_error) = emit_action_audit_event(
            state,
            claims,
            &action,
            None,
            body.target_object_id,
            "failure",
            "medium",
            Some(&error),
            body.justification.as_deref(),
            &body.parameters,
            None,
            None,
        )
        .await
        {
            log_audit_failure(action.id, &audit_error);
        }
        record_action_failure_metric(
            state,
            claims,
            action.id,
            body.target_object_id,
            &body.parameters,
            crate::metrics::FailureType::InvalidParameter,
            started_at,
            metrics_handle,
            &action_id_label,
        )
        .await;
        return invalid_action(error);
    }

    // TASK G — Writeback webhook fires synchronously before validation. A
    // failure aborts the action with `InvalidParameter` semantics so rules
    // never observe partial state. Captured `output_parameters` are merged
    // into `body.parameters` so downstream rules can reference them.
    if let Some(writeback_cfg) = webhook_writeback_cfg.as_ref() {
        if let Err(error) = run_webhook_writeback(state, writeback_cfg, &mut body.parameters).await
        {
            record_action_failure_metric(
                state,
                claims,
                action.id,
                body.target_object_id,
                &body.parameters,
                crate::metrics::FailureType::InvalidParameter,
                started_at,
                metrics_handle,
                &action_id_label,
            )
            .await;
            return invalid_action(format!("webhook writeback failed: {error}"));
        }
    }

    let validation_request = ValidateActionRequest {
        target_object_id: body.target_object_id,
        parameters: body.parameters.clone(),
    };
    let plan = match plan_action(state, claims, &action, &validation_request).await {
        Ok(plan) => plan,
        Err(errors) => {
            let denied = all_forbidden(&errors);
            let status = if denied { "denied" } else { "failure" };
            if let Err(audit_error) = emit_action_audit_event(
                state,
                claims,
                &action,
                None,
                body.target_object_id,
                status,
                "medium",
                Some("action validation failed"),
                body.justification.as_deref(),
                &body.parameters,
                None,
                Some(&json!({ "details": errors })),
            )
            .await
            {
                log_audit_failure(action.id, &audit_error);
            }
            let failure_type = if denied {
                crate::metrics::FailureType::Authentication
            } else {
                crate::metrics::FailureType::InvalidParameter
            };
            record_action_failure_metric(
                state,
                claims,
                action.id,
                body.target_object_id,
                &body.parameters,
                failure_type,
                started_at,
                metrics_handle,
                &action_id_label,
            )
            .await;
            let payload = Json(json!({ "error": "action validation failed", "details": errors }));
            return if denied {
                (StatusCode::FORBIDDEN, payload).into_response()
            } else {
                (StatusCode::BAD_REQUEST, payload).into_response()
            };
        }
    };

    let target_snapshot = target_snapshot_from_plan(&plan);

    match execute_plan(state, claims, &action, body.justification.as_deref(), plan).await {
        Ok(executed) => {
            let audit_result = json!({
                "deleted": executed.deleted,
                "object": executed.object,
                "link": executed.link,
                "result": executed.result,
            });
            if let Err(audit_error) = emit_action_audit_event(
                state,
                claims,
                &action,
                target_snapshot.as_ref(),
                executed.target_object_id,
                "success",
                "low",
                None,
                body.justification.as_deref(),
                &body.parameters,
                Some(&executed.preview),
                Some(&audit_result),
            )
            .await
            {
                log_audit_failure(action.id, &audit_error);
            }

            if let Err(notification_error) = emit_action_notifications(
                state,
                claims,
                &action,
                target_snapshot.as_ref(),
                &body.parameters,
                body.justification.as_deref(),
                &executed,
            )
            .await
            {
                log_notification_failure(action.id, &notification_error);
            }

            let response = Json(ExecuteActionResponse {
                action,
                target_object_id: executed.target_object_id,
                deleted: executed.deleted,
                preview: executed.preview,
                object: executed.object,
                link: executed.link,
                result: executed.result,
            })
            .into_response();
            record_action_success_metric(
                state,
                claims,
                action_id_for_metric,
                executed.target_object_id,
                &body.parameters,
                started_at,
                metrics_handle,
                &action_id_label,
            )
            .await;
            // TASK G — Side-effect webhooks fire after the plan applied
            // successfully. Failures are logged + persisted but never
            // propagate.
            run_webhook_side_effects(
                state,
                claims,
                claims.sub,
                action_id_for_metric,
                executed.target_object_id,
                &webhook_side_effect_cfgs,
                &body.parameters,
            )
            .await;
            response
        }
        Err(error) => {
            if let Err(audit_error) = emit_action_audit_event(
                state,
                claims,
                &action,
                target_snapshot.as_ref(),
                body.target_object_id,
                "failure",
                "high",
                Some(&error),
                body.justification.as_deref(),
                &body.parameters,
                None,
                None,
            )
            .await
            {
                log_audit_failure(action.id, &audit_error);
            }
            let failure_type = classify_execute_plan_error(&error);
            record_action_failure_metric(
                state,
                claims,
                action.id,
                body.target_object_id,
                &body.parameters,
                failure_type,
                started_at,
                metrics_handle,
                &action_id_label,
            )
            .await;
            db_error(error)
        }
    }
}

// ---------------------------------------------------------------------------
// TASK F helpers — failure classification + DB ledger inserts.
// ---------------------------------------------------------------------------

/// Coarse classification for errors surfaced by [`execute_plan`]. The kernel
/// error type is a string today, so we pattern-match a handful of well-known
/// substrings before falling back to `Unclassified`.
fn classify_execute_plan_error(error: &str) -> crate::metrics::FailureType {
    let lower = error.to_ascii_lowercase();
    if lower.contains("forbidden") || lower.contains("permission") || lower.contains("unauthorized")
    {
        crate::metrics::FailureType::Authentication
    } else if lower.contains("conflict") || lower.contains("unique") || lower.contains("duplicate")
    {
        crate::metrics::FailureType::Conflict
    } else if lower.contains("rate limit") || lower.contains("too many") || lower.contains("quota")
    {
        crate::metrics::FailureType::ScaleLimit
    } else if lower.contains("timeout") || lower.contains("upstream") || lower.contains("network") {
        crate::metrics::FailureType::SideEffect
    } else if lower.contains("function") {
        if lower.contains("user") {
            crate::metrics::FailureType::UserFacingFunction
        } else {
            crate::metrics::FailureType::Function
        }
    } else if lower.contains("invalid") || lower.contains("missing") || lower.contains("required") {
        crate::metrics::FailureType::InvalidParameter
    } else {
        crate::metrics::FailureType::Unclassified
    }
}

#[allow(clippy::too_many_arguments)]
async fn record_action_success_metric(
    state: &AppState,
    claims: &Claims,
    action_id: Uuid,
    target_object_id: Option<Uuid>,
    parameters: &Value,
    started_at: std::time::Instant,
    metrics: Option<&'static crate::metrics::ActionMetrics>,
    action_id_label: &str,
) {
    let elapsed = started_at.elapsed();
    let elapsed_ms = i64::try_from(elapsed.as_millis()).unwrap_or(i64::MAX);
    if let Some(metrics) = metrics {
        metrics.record_success(action_id_label, elapsed.as_secs_f64());
    }
    if let Err(error) = persist_action_attempt_row(
        state,
        claims,
        action_id,
        target_object_id,
        parameters,
        "success",
        None,
        elapsed_ms,
    )
    .await
    {
        tracing::warn!(action = %action_id, error = %error, "failed to persist action success attempt");
    }
}

#[allow(clippy::too_many_arguments)]
async fn record_action_failure_metric(
    state: &AppState,
    claims: &Claims,
    action_id: Uuid,
    target_object_id: Option<Uuid>,
    parameters: &Value,
    failure_type: crate::metrics::FailureType,
    started_at: std::time::Instant,
    metrics: Option<&'static crate::metrics::ActionMetrics>,
    action_id_label: &str,
) {
    let elapsed = started_at.elapsed();
    let elapsed_ms = i64::try_from(elapsed.as_millis()).unwrap_or(i64::MAX);
    if let Some(metrics) = metrics {
        metrics.record_failure(action_id_label, failure_type, elapsed.as_secs_f64());
    }
    if let Err(error) = persist_action_attempt_row(
        state,
        claims,
        action_id,
        target_object_id,
        parameters,
        "failure",
        Some(failure_type.as_str()),
        elapsed_ms,
    )
    .await
    {
        tracing::warn!(action = %action_id, error = %error, "failed to persist action failure attempt");
    }
}

/// Append an execution attempt to the Cassandra action log. Revertible
/// execution history still has its own residual PG path, but metrics no
/// longer depend on `action_executions`.
async fn persist_action_attempt_row(
    state: &AppState,
    claims: &Claims,
    action_id: Uuid,
    target_object_id: Option<Uuid>,
    parameters: &Value,
    status: &str,
    failure_type: Option<&str>,
    duration_ms: i64,
) -> Result<(), String> {
    let payload = json!({
        "action_type_id": action_id,
        "target_object_id": target_object_id,
        "parameters": parameters,
        "status": status,
        "failure_type": failure_type,
        "duration_ms": duration_ms,
        "organization_id": claims.org_id,
    });
    state
        .stores
        .actions
        .append(ActionLogEntry {
            tenant: tenant_from_claims(claims),
            event_id: Some(deterministic_action_event_id(&[
                "attempt".to_string(),
                action_id.to_string(),
                target_object_id
                    .map(|id| id.to_string())
                    .unwrap_or_else(|| "none".to_string()),
                claims.sub.to_string(),
                status.to_string(),
                serde_json::to_string(parameters).unwrap_or_default(),
            ])),
            action_id: action_id.to_string(),
            kind: "action_attempt".to_string(),
            subject: claims.sub.to_string(),
            object: target_object_id.map(|id| ObjectId(id.to_string())),
            payload,
            recorded_at_ms: Utc::now().timestamp_millis(),
        })
        .await
        .map_err(|e| format!("failed to append action attempt to action log: {e}"))
}

/// `GET /api/v1/ontology/actions/{id}/metrics?window=30d` — aggregates
/// Cassandra action-log attempts for the requested window and returns
/// success/failure counts plus P95 duration. The same data is also exported
/// as Prometheus counters.
pub async fn get_action_metrics(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(action_id): Path<Uuid>,
    Query(params): Query<ActionMetricsQuery>,
) -> impl IntoResponse {
    let window = params.window.as_deref().unwrap_or("30d").to_string();
    let window_seconds = match parse_window_to_seconds(&window) {
        Ok(secs) => secs,
        Err(message) => {
            return (StatusCode::BAD_REQUEST, Json(json!({ "error": message }))).into_response();
        }
    };

    let window_ms = match i64::try_from(window_seconds)
        .ok()
        .and_then(|seconds| seconds.checked_mul(1_000))
    {
        Some(ms) => ms,
        None => return db_error(format!("window overflow: {window}")),
    };
    let cutoff_ms = Utc::now().timestamp_millis().saturating_sub(window_ms);
    let tenant = tenant_from_claims(&claims);
    let action_id_filter = action_id.to_string();
    let mut success_count = 0_i64;
    let mut failure_count = 0_i64;
    let mut durations = Vec::new();
    let mut failure_categories = std::collections::BTreeMap::<String, i64>::new();
    let mut next_token = None;
    let mut reached_cutoff = false;

    loop {
        let page = match state
            .stores
            .actions
            .list_recent(
                &tenant,
                Page {
                    size: 5_000,
                    token: next_token.take(),
                },
                ReadConsistency::Eventual,
            )
            .await
        {
            Ok(page) => page,
            Err(error) => return db_error(format!("failed to aggregate action metrics: {error}")),
        };

        for entry in page.items {
            if entry.recorded_at_ms < cutoff_ms {
                reached_cutoff = true;
                break;
            }
            if entry.kind != "action_attempt" {
                continue;
            }
            let Some(payload_action_id) =
                entry.payload.get("action_type_id").and_then(Value::as_str)
            else {
                continue;
            };
            if payload_action_id != action_id_filter {
                continue;
            }

            match entry.payload.get("status").and_then(Value::as_str) {
                Some("success") => success_count += 1,
                Some("failure") => {
                    failure_count += 1;
                    if let Some(failure_type) =
                        entry.payload.get("failure_type").and_then(Value::as_str)
                    {
                        *failure_categories
                            .entry(failure_type.to_string())
                            .or_insert(0) += 1;
                    }
                }
                _ => {}
            }
            if let Some(duration_ms) = entry.payload.get("duration_ms").and_then(Value::as_f64) {
                durations.push(duration_ms);
            }
        }

        if reached_cutoff {
            break;
        }
        match page.next_token {
            Some(token) => next_token = Some(token),
            None => break,
        }
    }

    Json(ActionMetricsResponse {
        action_id,
        window,
        success_count,
        failure_count,
        p95_duration_ms: percentile_95_duration_ms(durations),
        failure_categories,
    })
    .into_response()
}

fn percentile_95_duration_ms(mut samples: Vec<f64>) -> Option<f64> {
    if samples.is_empty() {
        return None;
    }
    samples.sort_by(|a, b| a.total_cmp(b));
    let rank = 0.95 * (samples.len().saturating_sub(1) as f64);
    let lower = rank.floor() as usize;
    let upper = rank.ceil() as usize;
    if lower == upper {
        return samples.get(lower).copied();
    }
    let lower_value = samples[lower];
    let upper_value = samples[upper];
    Some(lower_value + (upper_value - lower_value) * (rank - lower as f64))
}

/// Parse strings like `30d`, `12h`, `45m`, `120s`, `2w`. Returns the window
/// in seconds. A bare numeric value is treated as days.
fn parse_window_to_seconds(input: &str) -> Result<u64, String> {
    let trimmed = input.trim();
    if trimmed.is_empty() {
        return Err("window must not be empty".into());
    }
    let (number_part, suffix) = match trimmed.char_indices().find(|(_, c)| !c.is_ascii_digit()) {
        Some((idx, _)) => (&trimmed[..idx], &trimmed[idx..]),
        None => (trimmed, "d"),
    };
    let value: u64 = number_part
        .parse()
        .map_err(|_| format!("invalid window value: {input}"))?;
    let multiplier: u64 = match suffix {
        "s" => 1,
        "m" => 60,
        "h" => 3_600,
        "d" => 86_400,
        "w" => 7 * 86_400,
        other => return Err(format!("unsupported window suffix: {other}")),
    };
    value
        .checked_mul(multiplier)
        .ok_or_else(|| format!("window overflow: {input}"))
}

#[derive(Deserialize)]
pub struct ActionMetricsQuery {
    pub window: Option<String>,
}

#[derive(Serialize)]
pub struct ActionMetricsResponse {
    pub action_id: Uuid,
    pub window: String,
    pub success_count: i64,
    pub failure_count: i64,
    pub p95_duration_ms: Option<f64>,
    pub failure_categories: std::collections::BTreeMap<String, i64>,
}

pub async fn execute_action(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<ExecuteActionRequest>,
) -> impl IntoResponse {
    let Some(row) = (match load_action_row(&state, id).await {
        Ok(row) => row,
        Err(e) => return db_error(format!("failed to load action type: {e}")),
    }) else {
        return StatusCode::NOT_FOUND.into_response();
    };

    let action = match ActionType::try_from(row) {
        Ok(action_type) => action_type,
        Err(e) => return db_error(format!("failed to decode action type: {e}")),
    };

    execute_loaded_action(&state, &claims, action, body).await
}

pub async fn execute_inline_edit(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((type_id, obj_id, property_id)): Path<(Uuid, Uuid, Uuid)>,
    Json(body): Json<ExecuteInlineEditRequest>,
) -> impl IntoResponse {
    let property = match load_property_row(&state, type_id, property_id).await {
        Ok(Some(property)) => property,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => return db_error(format!("failed to load property: {error}")),
    };

    let Some(inline_edit_config) = property.inline_edit_config.clone() else {
        return invalid_action("inline edit is not configured for this property");
    };

    let target = match load_and_authorize_target(&state, &claims, obj_id, type_id).await {
        Ok(target) => target,
        Err(errors) => {
            return if all_forbidden(&errors) {
                forbidden(errors.join("; "))
            } else {
                invalid_action(errors.join("; "))
            };
        }
    };

    let Some(row) = (match load_action_row(&state, inline_edit_config.action_type_id).await {
        Ok(row) => row,
        Err(error) => return db_error(format!("failed to load inline edit action: {error}")),
    }) else {
        return invalid_action("configured inline edit action type was not found");
    };

    let action = match ActionType::try_from(row) {
        Ok(action_type) => action_type,
        Err(error) => return db_error(format!("failed to decode inline edit action: {error}")),
    };

    if action.object_type_id != type_id {
        return invalid_action(
            "configured inline edit action type no longer belongs to this object type",
        );
    }

    let parameters = match build_inline_edit_parameters(
        &action,
        &property,
        &target,
        &inline_edit_config,
        body.value,
    ) {
        Ok(parameters) => parameters,
        Err(error) => return invalid_action(error),
    };

    execute_loaded_action(
        &state,
        &claims,
        action,
        ExecuteActionRequest {
            target_object_id: Some(obj_id),
            parameters,
            justification: body.justification,
        },
    )
    .await
}

/// TASK L — Bulk inline-edit endpoint. Each entry is validated and
/// executed individually (per-edit submission criteria evaluation, per
/// `Inline edits.md`); the request is rejected up front if two entries
/// target the same `object_id` (Foundry "Invalid inline Actions").
///
/// `POST /api/v1/ontology/types/{type_id}/inline-edit-batch`
pub async fn execute_inline_edit_batch(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(type_id): Path<Uuid>,
    Json(body): Json<crate::models::property::ExecuteInlineEditBatchRequest>,
) -> impl IntoResponse {
    if body.edits.is_empty() {
        return invalid_action("edits must not be empty");
    }

    // TASK M — Enforce documented Foundry scale limits before doing any work.
    if body.edits.len() > scale_limits::MAX_OBJECTS_PER_SUBMISSION {
        return scale_limit_response(format!(
            "inline edit batch contains {} entries which exceeds the per-submission limit ({} max)",
            body.edits.len(),
            scale_limits::MAX_OBJECTS_PER_SUBMISSION
        ));
    }
    for edit in &body.edits {
        let edit_bytes = estimate_edit_bytes(&edit.value);
        if edit_bytes > scale_limits::MAX_EDIT_BYTES {
            return scale_limit_response(format!(
                "inline edit for object {} is {} bytes which exceeds the per-edit limit ({} bytes)",
                edit.object_id,
                edit_bytes,
                scale_limits::MAX_EDIT_BYTES
            ));
        }
    }

    // Reject duplicates on the same object.
    let mut seen_objects = HashSet::new();
    for edit in &body.edits {
        if !seen_objects.insert(edit.object_id) {
            return invalid_action(format!(
                "inline edit batch contains two edits targeting the same object {} (rejected; see Inline edits documentation)",
                edit.object_id
            ));
        }
    }

    let total = body.edits.len();
    let mut results = Vec::with_capacity(total);
    let mut succeeded = 0usize;

    for edit in body.edits {
        let property = match load_property_row(&state, type_id, edit.property_id).await {
            Ok(Some(property)) => property,
            Ok(None) => {
                results.push(json!({
                    "property_id": edit.property_id,
                    "object_id": edit.object_id,
                    "status": "failure",
                    "error": "property not found"
                }));
                continue;
            }
            Err(error) => {
                results.push(json!({
                    "property_id": edit.property_id,
                    "object_id": edit.object_id,
                    "status": "failure",
                    "error": format!("failed to load property: {error}")
                }));
                continue;
            }
        };

        let Some(inline_edit_config) = property.inline_edit_config.clone() else {
            results.push(json!({
                "property_id": edit.property_id,
                "object_id": edit.object_id,
                "status": "failure",
                "error": "inline edit not configured for this property"
            }));
            continue;
        };

        let target = match load_and_authorize_target(&state, &claims, edit.object_id, type_id).await
        {
            Ok(target) => target,
            Err(errors) => {
                results.push(json!({
                    "property_id": edit.property_id,
                    "object_id": edit.object_id,
                    "status": "failure",
                    "error": errors.join("; ")
                }));
                continue;
            }
        };

        let action_row = match load_action_row(&state, inline_edit_config.action_type_id).await {
            Ok(Some(row)) => row,
            Ok(None) => {
                results.push(json!({
                    "property_id": edit.property_id,
                    "object_id": edit.object_id,
                    "status": "failure",
                    "error": "configured inline edit action type was not found"
                }));
                continue;
            }
            Err(error) => {
                results.push(json!({
                    "property_id": edit.property_id,
                    "object_id": edit.object_id,
                    "status": "failure",
                    "error": format!("failed to load inline edit action: {error}")
                }));
                continue;
            }
        };

        let action = match ActionType::try_from(action_row) {
            Ok(action_type) => action_type,
            Err(error) => {
                results.push(json!({
                    "property_id": edit.property_id,
                    "object_id": edit.object_id,
                    "status": "failure",
                    "error": format!("failed to decode inline edit action: {error}")
                }));
                continue;
            }
        };

        if action.object_type_id != type_id {
            results.push(json!({
                "property_id": edit.property_id,
                "object_id": edit.object_id,
                "status": "failure",
                "error": "configured inline edit action no longer belongs to this object type"
            }));
            continue;
        }

        let parameters = match build_inline_edit_parameters(
            &action,
            &property,
            &target,
            &inline_edit_config,
            edit.value,
        ) {
            Ok(parameters) => parameters,
            Err(error) => {
                results.push(json!({
                    "property_id": edit.property_id,
                    "object_id": edit.object_id,
                    "status": "failure",
                    "error": error
                }));
                continue;
            }
        };

        let request = ExecuteActionRequest {
            target_object_id: Some(edit.object_id),
            parameters,
            justification: edit.justification,
        };

        let response = execute_loaded_action(&state, &claims, action, request).await;
        let status = response.status();
        if status.is_success() {
            succeeded += 1;
            results.push(json!({
                "property_id": edit.property_id,
                "object_id": edit.object_id,
                "status": "success"
            }));
        } else {
            results.push(json!({
                "property_id": edit.property_id,
                "object_id": edit.object_id,
                "status": "failure",
                "http_status": status.as_u16()
            }));
        }
    }

    Json(json!({
        "total": total,
        "succeeded": succeeded,
        "failed": total - succeeded,
        "results": results
    }))
    .into_response()
}

/// TASK L — Re-validate inline-edit requirements when an action that is
/// referenced by `properties.inline_edit_config` is updated. Currently
/// enforced: same-object-type, `update_object` operation, no side-effect
/// webhooks/notifications.
async fn ensure_inline_edit_requirements_for_action(
    state: &AppState,
    action_id: Uuid,
    operation_kind: &str,
    config: &Value,
) -> Result<(), String> {
    let referencing = action_repository::action_has_inline_edit_references(
        state.stores.definitions.as_ref(),
        action_id,
    )
    .await
    .map_err(|error| format!("failed to scan inline edit references: {error}"))?;

    if !referencing {
        return Ok(());
    }

    if operation_kind != "update_object" {
        return Err(
            "this action is referenced as an inline edit; operation_kind must remain update_object"
                .to_string(),
        );
    }

    if let Some(object) = config.as_object() {
        if let Some(notifications) = object.get("notification_side_effects") {
            let non_empty = notifications
                .as_array()
                .map(|items| !items.is_empty())
                .unwrap_or(false);
            if non_empty {
                return Err(
                    "this action is referenced as an inline edit; side-effect notifications must remain disabled"
                        .to_string(),
                );
            }
        }
        if let Some(webhooks) = object.get("webhook_side_effects") {
            let non_empty = webhooks
                .as_array()
                .map(|items| !items.is_empty())
                .unwrap_or(false);
            if non_empty {
                return Err(
                    "this action is referenced as an inline edit; side-effect webhooks must remain disabled"
                        .to_string(),
                );
            }
        }
    }

    Ok(())
}

pub async fn execute_action_batch(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<ExecuteBatchActionRequest>,
) -> impl IntoResponse {
    if body.target_object_ids.is_empty() {
        return invalid_action("target_object_ids must not be empty");
    }

    let Some(row) = (match load_action_row(&state, id).await {
        Ok(row) => row,
        Err(e) => return db_error(format!("failed to load action type: {e}")),
    }) else {
        return StatusCode::NOT_FOUND.into_response();
    };

    let action = match ActionType::try_from(row) {
        Ok(action_type) => action_type,
        Err(e) => return db_error(format!("failed to decode action type: {e}")),
    };

    // TASK H — Function-backed actions can opt into batched execution. The
    // size cap follows the documented Foundry limits (Scale and property
    // limits.md): 20 targets in single-call mode, 10 000 when batched.
    let batched = extract_batched_execution_flag(&action.config);
    // TASK M — Per `Scale and property limits.md`, the 20-target cap only
    // applies to function-backed actions running without batched_execution.
    // Every other configuration accepts up to MAX_OBJECTS_PER_SUBMISSION.
    let function_backed = parse_operation_kind(&action.operation_kind)
        .map(|kind| kind == ActionOperationKind::InvokeFunction)
        .unwrap_or(false);
    let limit = if function_backed && !batched {
        DEFAULT_BATCH_MAX_TARGETS
    } else {
        scale_limits::MAX_OBJECTS_PER_SUBMISSION
    };
    if body.target_object_ids.len() > limit {
        return scale_limit_response(format!(
            "target_object_ids exceeds the per-call scale limit ({} max in {} mode)",
            limit,
            if function_backed && !batched {
                "function single-call"
            } else if batched {
                "batched"
            } else {
                "standard"
            }
        ));
    }

    // TASK M — Each per-target edit must remain within OSv2's 3 MB ceiling.
    // We size-check the parameters payload once since it is shared across
    // every target in the batch.
    let parameters_bytes = estimate_edit_bytes(&body.parameters);
    if parameters_bytes > scale_limits::MAX_EDIT_BYTES {
        return scale_limit_response(format!(
            "parameters payload of {} bytes exceeds the per-edit scale limit ({} bytes)",
            parameters_bytes,
            scale_limits::MAX_EDIT_BYTES
        ));
    }
    if let Err(error) = validate_parameter_list_sizes(&action.input_schema, &body.parameters) {
        return scale_limit_response(error);
    }

    if let Err(error) = ensure_action_actor_permission(&claims, &action) {
        if let Err(audit_error) = emit_action_audit_event(
            &state,
            &claims,
            &action,
            None,
            None,
            "denied",
            "medium",
            Some(&error),
            body.justification.as_deref(),
            &body.parameters,
            None,
            Some(&json!({ "target_count": body.target_object_ids.len() })),
        )
        .await
        {
            log_audit_failure(action.id, &audit_error);
        }
        return forbidden(error);
    }

    if let Err(error) = ensure_confirmation_justification(&action, body.justification.as_deref()) {
        if let Err(audit_error) = emit_action_audit_event(
            &state,
            &claims,
            &action,
            None,
            None,
            "failure",
            "medium",
            Some(&error),
            body.justification.as_deref(),
            &body.parameters,
            None,
            Some(&json!({ "target_count": body.target_object_ids.len() })),
        )
        .await
        {
            log_audit_failure(action.id, &audit_error);
        }
        return invalid_action(error);
    }

    let total = body.target_object_ids.len();
    let mut succeeded = 0usize;
    let mut results = Vec::with_capacity(total);

    // TASK H — Batched function-invocation path: collapse N validations + N
    // function calls into a single invocation with `params = { batch: [...] }`.
    // Only valid when the action is function-backed; for any other operation
    // kind we fall back to the per-target loop.
    if batched
        && parse_operation_kind(&action.operation_kind)
            .map(|kind| kind == ActionOperationKind::InvokeFunction)
            .unwrap_or(false)
    {
        let outcome = execute_batched_function_invocation(
            &state,
            &claims,
            &action,
            &body.target_object_ids,
            &body.parameters,
            body.justification.as_deref(),
        )
        .await;
        return match outcome {
            Ok(value) => Json(json!({
                "total": total,
                "succeeded": total,
                "failed": 0,
                "batched": true,
                "result": value,
            }))
            .into_response(),
            Err(error) => {
                if let Err(audit_error) = emit_action_audit_event(
                    &state,
                    &claims,
                    &action,
                    None,
                    None,
                    "failure",
                    "high",
                    Some(&error),
                    body.justification.as_deref(),
                    &body.parameters,
                    None,
                    Some(&json!({ "batched": true, "target_count": total })),
                )
                .await
                {
                    log_audit_failure(action.id, &audit_error);
                }
                db_error(error)
            }
        };
    }

    for target_object_id in body.target_object_ids {
        let validation_request = ValidateActionRequest {
            target_object_id: Some(target_object_id),
            parameters: body.parameters.clone(),
        };

        match plan_action(&state, &claims, &action, &validation_request).await {
            Ok(plan) => {
                let target_snapshot = target_snapshot_from_plan(&plan);

                match execute_plan(
                    &state,
                    &claims,
                    &action,
                    body.justification.as_deref(),
                    plan,
                )
                .await
                {
                    Ok(executed) => {
                        succeeded += 1;
                        let audit_result = json!({
                            "deleted": executed.deleted,
                            "object": executed.object,
                            "link": executed.link,
                            "result": executed.result,
                            "batch": true,
                        });
                        if let Err(audit_error) = emit_action_audit_event(
                            &state,
                            &claims,
                            &action,
                            target_snapshot.as_ref(),
                            executed.target_object_id,
                            "success",
                            "low",
                            None,
                            body.justification.as_deref(),
                            &body.parameters,
                            Some(&executed.preview),
                            Some(&audit_result),
                        )
                        .await
                        {
                            log_audit_failure(action.id, &audit_error);
                        }

                        if let Err(notification_error) = emit_action_notifications(
                            &state,
                            &claims,
                            &action,
                            target_snapshot.as_ref(),
                            &body.parameters,
                            body.justification.as_deref(),
                            &executed,
                        )
                        .await
                        {
                            log_notification_failure(action.id, &notification_error);
                        }

                        results.push(json!({
                            "target_object_id": target_object_id,
                            "status": "succeeded",
                            "deleted": executed.deleted,
                            "preview": executed.preview,
                            "object": executed.object,
                            "link": executed.link,
                            "result": executed.result,
                        }));
                    }
                    Err(error) => {
                        if let Err(audit_error) = emit_action_audit_event(
                            &state,
                            &claims,
                            &action,
                            target_snapshot.as_ref(),
                            Some(target_object_id),
                            "failure",
                            "high",
                            Some(&error),
                            body.justification.as_deref(),
                            &body.parameters,
                            None,
                            Some(&json!({ "batch": true })),
                        )
                        .await
                        {
                            log_audit_failure(action.id, &audit_error);
                        }

                        results.push(json!({
                            "target_object_id": target_object_id,
                            "status": "failed",
                            "error": error,
                        }));
                    }
                }
            }
            Err(errors) => {
                let denied = all_forbidden(&errors);
                if let Err(audit_error) = emit_action_audit_event(
                    &state,
                    &claims,
                    &action,
                    None,
                    Some(target_object_id),
                    if denied { "denied" } else { "failure" },
                    "medium",
                    Some("action validation failed"),
                    body.justification.as_deref(),
                    &body.parameters,
                    None,
                    Some(&json!({ "details": errors, "batch": true })),
                )
                .await
                {
                    log_audit_failure(action.id, &audit_error);
                }

                results.push(json!({
                    "target_object_id": target_object_id,
                    "status": if denied { "denied" } else { "failed" },
                    "errors": errors,
                }));
            }
        }
    }

    Json(ExecuteBatchActionResponse {
        action,
        total,
        succeeded,
        failed: total.saturating_sub(succeeded),
        results,
    })
    .into_response()
}

pub async fn create_action_what_if_branch(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<CreateActionWhatIfBranchRequest>,
) -> impl IntoResponse {
    let Some(row) = (match load_action_row(&state, id).await {
        Ok(row) => row,
        Err(error) => return db_error(format!("failed to load action type: {error}")),
    }) else {
        return StatusCode::NOT_FOUND.into_response();
    };

    let action = match ActionType::try_from(row) {
        Ok(action_type) => action_type,
        Err(error) => return db_error(format!("failed to decode action type: {error}")),
    };

    if let Err(error) = ensure_action_actor_permission(&claims, &action) {
        return forbidden(error);
    }

    let validation_request = ValidateActionRequest {
        target_object_id: body.target_object_id,
        parameters: body.parameters.clone(),
    };
    let plan = match plan_action(&state, &claims, &action, &validation_request).await {
        Ok(plan) => plan,
        Err(errors) => {
            let payload = Json(json!({ "error": "action validation failed", "details": errors }));
            return if all_forbidden(&errors) {
                (StatusCode::FORBIDDEN, payload).into_response()
            } else {
                (StatusCode::BAD_REQUEST, payload).into_response()
            };
        }
    };

    let preview = plan_preview(&plan);
    let target_snapshot = target_snapshot_from_plan(&plan);
    let before_object = target_snapshot.clone().map(|target| json!(target));
    let after_object = match target_snapshot.as_ref() {
        Some(target) => match simulate_target_after_preview(&state, target, &preview).await {
            Ok(after) => after,
            Err(error) => return invalid_action(error),
        },
        None => None,
    };
    let deleted = after_object.is_none() && target_snapshot.is_some();
    let branch_name = body.name.unwrap_or_else(|| {
        format!(
            "{} what-if {}",
            action.display_name,
            chrono::Utc::now().format("%Y-%m-%d %H:%M:%S")
        )
    });

    let now = Utc::now();
    let branch = ActionWhatIfBranch {
        id: Uuid::now_v7(),
        action_id: action.id,
        target_object_id: body.target_object_id,
        name: branch_name,
        description: body.description.unwrap_or_default(),
        parameters: body.parameters,
        preview,
        before_object,
        after_object,
        deleted,
        owner_id: claims.sub,
        created_at: now,
        updated_at: now,
    };

    match action_repository::create_what_if_branch(
        state.stores.read_models.as_ref(),
        tenant_from_claims(&claims),
        branch,
    )
    .await
    {
        Ok(branch) => (StatusCode::CREATED, Json(json!(branch))).into_response(),
        Err(error) => db_error(format!("failed to create action what-if branch: {error}")),
    }
}

/// TASK P — `POST /api/v1/ontology/actions/uploads`. Accepts a small JSON
/// body describing a file (`filename`, `content_type`, `size_bytes`,
/// optional inline `content_base64` for tiny payloads) and returns an
/// opaque `attachment_rid` plus a stable storage URI. Action parameters of
/// type `attachment` or `media_reference` use the returned identifier as
/// their value.
///
/// This endpoint is intentionally minimal: full Foundry parity (chunked
/// uploads, signed URLs, virus scans) is delegated to
/// `data-asset-catalog-service`. The current implementation enforces the
/// 3 MB per-edit ceiling defined in `Scale and property limits.md`.
#[derive(Debug, Deserialize)]
pub struct UploadAttachmentRequest {
    pub filename: String,
    #[serde(default)]
    pub content_type: Option<String>,
    /// Total file size in bytes. Required so we can apply the per-edit cap
    /// even when callers stream the body separately to object storage.
    pub size_bytes: u64,
    /// Optional inline payload, base64-encoded. Mostly used by tests; the
    /// production flow uploads to object storage and skips this field.
    #[serde(default)]
    pub content_base64: Option<String>,
}

pub async fn upload_action_attachment(
    AuthUser(_claims): AuthUser,
    State(_state): State<AppState>,
    Json(request): Json<UploadAttachmentRequest>,
) -> impl IntoResponse {
    if request.filename.trim().is_empty() {
        return invalid_action("filename is required");
    }
    if request.size_bytes == 0 {
        return invalid_action("size_bytes must be greater than zero");
    }
    if request.size_bytes as usize > scale_limits::MAX_EDIT_BYTES {
        return scale_limit_response(format!(
            "attachment of {} bytes exceeds the per-edit scale limit ({} bytes)",
            request.size_bytes,
            scale_limits::MAX_EDIT_BYTES
        ));
    }
    if let Some(payload) = request.content_base64.as_deref() {
        // Very rough sanity check: base64 expands by 4/3, so reject obvious
        // overruns without performing the decode.
        if payload.len() / 4 * 3 > scale_limits::MAX_EDIT_BYTES {
            return scale_limit_response("inline attachment payload exceeds the per-edit limit");
        }
    }

    let attachment_rid = format!("ri.attachments.{}", Uuid::now_v7());
    Json(json!({
        "attachment_rid": attachment_rid,
        "filename": request.filename,
        "content_type": request.content_type,
        "size_bytes": request.size_bytes,
        // Storage URI is opaque to clients; the data-asset-catalog-service
        // resolves it to a presigned download URL on demand.
        "storage_uri": format!("attachments://{attachment_rid}"),
    }))
    .into_response()
}

pub async fn list_action_what_if_branches(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Query(params): Query<ListActionWhatIfBranchesQuery>,
) -> impl IntoResponse {
    let page = params.page.unwrap_or(1).max(1);
    let per_page = params.per_page.unwrap_or(20).clamp(1, 100);
    let offset = (page - 1) * per_page;
    let show_all = claims.has_role("admin");

    let tenant = tenant_from_claims(&claims);
    let total = action_repository::count_what_if_branches(
        state.stores.read_models.as_ref(),
        action_repository::WhatIfListQuery {
            tenant: tenant.clone(),
            action_id: id,
            target_object_id: params.target_object_id,
            owner_id: claims.sub,
            show_all,
            page: Page {
                size: 10_000,
                token: None,
            },
        },
    )
    .await
    .unwrap_or(0) as i64;

    let data = action_repository::list_what_if_branches(
        state.stores.read_models.as_ref(),
        action_repository::WhatIfListQuery {
            tenant,
            action_id: id,
            target_object_id: params.target_object_id,
            owner_id: claims.sub,
            show_all,
            page: Page {
                size: per_page as u32,
                token: Some(offset.to_string()),
            },
        },
    )
    .await
    .map(|page| page.items)
    .unwrap_or_default();

    Json(ListActionWhatIfBranchesResponse {
        data,
        total,
        page,
        per_page,
    })
    .into_response()
}

pub async fn delete_action_what_if_branch(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((id, branch_id)): Path<(Uuid, Uuid)>,
) -> impl IntoResponse {
    match action_repository::delete_what_if_branch(
        state.stores.read_models.as_ref(),
        &tenant_from_claims(&claims),
        id,
        branch_id,
        claims.sub,
        claims.has_role("admin"),
    )
    .await
    {
        Ok(true) => StatusCode::NO_CONTENT.into_response(),
        Ok(false) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => db_error(format!("failed to delete what-if branch: {error}")),
    }
}

/// TASK G — Invokes a webhook registered in
/// `connector-management-service` with the supplied input payload. Returns
/// the structured response so the caller can surface `output_parameters`
/// to subsequent rules. Errors are bubbled up so writeback can abort the
/// action while side-effect calls can swallow them.
async fn invoke_registered_webhook(
    state: &AppState,
    webhook: &WebhookCallConfig,
    action_parameters: &Value,
) -> Result<Value, String> {
    if state.connector_management_service_url.trim().is_empty() {
        return Err("connector_management_service_url is not configured".to_string());
    }

    let mut inputs = serde_json::Map::new();
    if let Some(map) = action_parameters.as_object() {
        for mapping in &webhook.input_mappings {
            if let Some(value) = map.get(&mapping.action_input_name) {
                inputs.insert(mapping.webhook_input_name.clone(), value.clone());
            }
        }
    }

    let url = format!(
        "{}/api/v1/webhooks/{}/invoke",
        state.connector_management_service_url.trim_end_matches('/'),
        webhook.webhook_id
    );
    let response = state
        .http_client
        .post(&url)
        .json(&json!({ "inputs": Value::Object(inputs) }))
        .send()
        .await
        .map_err(|error| format!("webhook invocation failed: {error}"))?;
    let status = response.status();
    let text = response
        .text()
        .await
        .map_err(|error| format!("failed to read webhook response: {error}"))?;
    if !status.is_success() {
        return Err(format!("webhook returned {status}: {text}"));
    }
    if text.trim().is_empty() {
        Ok(Value::Null)
    } else {
        Ok(serde_json::from_str(&text).unwrap_or(Value::String(text)))
    }
}

/// TASK G — Invokes the writeback webhook (if any) before [`plan_action`].
/// On success, the response's `output_parameters` (if present) are merged
/// into the action's parameter map under either the configured alias or a
/// flat `webhook_output` key so subsequent rules can reference them.
async fn run_webhook_writeback(
    state: &AppState,
    config: &WebhookCallConfig,
    parameters: &mut Value,
) -> Result<(), String> {
    let response = invoke_registered_webhook(state, config, parameters).await?;
    let output = response
        .as_object()
        .and_then(|map| map.get("output_parameters"))
        .cloned()
        .unwrap_or(response);
    if !parameters.is_object() {
        *parameters = json!({});
    }
    let alias = config
        .output_parameter_alias
        .as_deref()
        .unwrap_or("webhook_output");
    if let Some(map) = parameters.as_object_mut() {
        map.insert(alias.to_string(), output);
    }
    Ok(())
}

/// TASK G — Persist a side-effect webhook invocation row. Best-effort: a
/// failure here only logs because side effects are non-blocking by design.
async fn persist_webhook_side_effect_row(
    state: &AppState,
    claims: &Claims,
    action_id: Uuid,
    target_object_id: Option<Uuid>,
    webhook_id: Uuid,
    actor: Uuid,
    status: &str,
    response: Option<&Value>,
    error: Option<&str>,
) {
    let payload = json!({
        "action_type_id": action_id,
        "side_effect_type": "webhook",
        "webhook_id": webhook_id,
        "actor_id": actor,
        "status": status,
        "response": response.cloned().unwrap_or(Value::Null),
        "error_message": error,
        "organization_id": claims.org_id,
    });

    if let Err(insert_error) = state
        .stores
        .actions
        .append(ActionLogEntry {
            tenant: tenant_from_claims(claims),
            event_id: Some(deterministic_action_event_id(&[
                "side_effect".to_string(),
                action_id.to_string(),
                target_object_id
                    .map(|id| id.to_string())
                    .unwrap_or_else(|| "none".to_string()),
                webhook_id.to_string(),
                actor.to_string(),
                status.to_string(),
            ])),
            action_id: action_id.to_string(),
            kind: "side_effect".to_string(),
            subject: actor.to_string(),
            object: target_object_id.map(|id| ObjectId(id.to_string())),
            payload,
            recorded_at_ms: Utc::now().timestamp_millis(),
        })
        .await
    {
        tracing::warn!(action = %action_id, webhook = %webhook_id, error = %insert_error, "failed to append side-effect ledger row to action log");
    }
}

/// TASK G — Fan out the configured side-effect webhooks in parallel via
/// `futures::future::join_all`. Errors are logged + persisted but never
/// propagated; this matches Foundry's "fire-and-forget" semantics.
async fn run_webhook_side_effects(
    state: &AppState,
    claims: &Claims,
    actor: Uuid,
    action_id: Uuid,
    target_object_id: Option<Uuid>,
    configs: &[WebhookCallConfig],
    parameters: &Value,
) {
    if configs.is_empty() {
        return;
    }
    let futures = configs.iter().map(|config| async move {
        match invoke_registered_webhook(state, config, parameters).await {
            Ok(response) => {
                persist_webhook_side_effect_row(
                    state,
                    claims,
                    action_id,
                    target_object_id,
                    config.webhook_id,
                    actor,
                    "success",
                    Some(&response),
                    None,
                )
                .await;
            }
            Err(error) => {
                tracing::warn!(action = %action_id, webhook = %config.webhook_id, error = %error, "webhook side-effect failed");
                persist_webhook_side_effect_row(
                    state,
                    claims,
                    action_id,
                    target_object_id,
                    config.webhook_id,
                    actor,
                    "failure",
                    None,
                    Some(error.as_str()),
                )
                .await;
            }
        }
    });
    futures::future::join_all(futures).await;
}

/// TASK H — single-call batch limit when `batched_execution = false`.
/// Mirrors Foundry's documented per-request cap.
pub const DEFAULT_BATCH_MAX_TARGETS: usize = 20;

/// TASK H — batched-execution cap; only honoured when the action is
/// function-backed and `batched_execution = true`.
pub const BATCHED_EXECUTION_MAX_TARGETS: usize = 10_000;

/// TASK M — Scale & property limits (Foundry `Scale and property limits.md`).
/// These constants centralise the hard caps shared by `execute_action_batch`
/// and `execute_inline_edit_batch` so all enforcement paths agree on the
/// numeric thresholds and the failure_type classification.
pub mod scale_limits {
    /// Maximum object types touched by a single submission (action batch or
    /// inline-edit batch). Currently each batch endpoint operates on a single
    /// object type by URL, but the helper enforces it explicitly so that any
    /// future multi-type submission picks up the cap automatically.
    pub const MAX_OBJECT_TYPES_PER_SUBMISSION: usize = 50;

    /// Maximum number of object instances edited within a single submission.
    pub const MAX_OBJECTS_PER_SUBMISSION: usize = 10_000;

    /// Maximum serialized size (bytes) for an individual edit's parameters or
    /// inline-edit value. Three megabytes mirrors OSv2's per-edit ceiling.
    pub const MAX_EDIT_BYTES: usize = 3 * 1024 * 1024;

    /// Maximum length of a primitive list parameter.
    pub const MAX_LIST_PRIMITIVE: usize = 10_000;

    /// Maximum length of an object reference list parameter.
    pub const MAX_OBJECT_REFERENCE_LIST: usize = 1_000;

    /// Maximum number of notification recipients resolved per side-effect
    /// configuration. Function-backed recipient resolution lowers this cap
    /// (see [`MAX_NOTIFICATION_RECIPIENTS_FROM_FUNCTION`]).
    pub const MAX_NOTIFICATION_RECIPIENTS: usize = 500;

    /// Cap when notification recipients come "from a function" (Foundry's
    /// `target_user_property_name` resolution path executed by a function).
    pub const MAX_NOTIFICATION_RECIPIENTS_FROM_FUNCTION: usize = 50;
}

/// TASK M — Validate list-shaped parameters against the documented Foundry
/// caps. `object_reference_list` follows the stricter 1 000-element limit;
/// every other list-typed parameter (including primitive arrays) caps at
/// 10 000 elements. Returns the canonical scale-limit message used by
/// [`scale_limit_response`].
pub fn validate_parameter_list_sizes(
    input_schema: &[ActionInputField],
    parameters: &Value,
) -> Result<(), String> {
    let Some(map) = parameters.as_object() else {
        return Ok(());
    };
    for field in input_schema {
        let Some(value) = map.get(&field.name) else {
            continue;
        };
        let Some(items) = value.as_array() else {
            continue;
        };
        let limit = match field.property_type.as_str() {
            "object_reference_list" => scale_limits::MAX_OBJECT_REFERENCE_LIST,
            "array" | "vector" => scale_limits::MAX_LIST_PRIMITIVE,
            other if other.ends_with("_list") => scale_limits::MAX_LIST_PRIMITIVE,
            _ => continue,
        };
        if items.len() > limit {
            return Err(format!(
                "parameter '{}' exceeds the scale limit ({} items, max {})",
                field.name,
                items.len(),
                limit
            ));
        }
    }
    Ok(())
}

/// TASK M — Estimate the serialized byte size of an edit's value/parameters
/// for the per-edit 3 MB cap. We use the canonical JSON representation since
/// that is what eventually crosses the wire to the writeback layer.
pub fn estimate_edit_bytes(value: &Value) -> usize {
    serde_json::to_vec(value).map(|v| v.len()).unwrap_or(0)
}

/// TASK H — Issue a single function invocation that receives the full batch
/// in `parameters.batch = [...]`. Required signature on the user function:
/// `(batch: List<Struct>) -> void`. Validation in [`validate_action_config`]
/// ensures the action only opts into batched execution when paired with a
/// function invocation (any other operation kind rejects the flag).
async fn execute_batched_function_invocation(
    state: &AppState,
    claims: &Claims,
    action: &ActionType,
    target_object_ids: &[Uuid],
    parameters: &Value,
    justification: Option<&str>,
) -> Result<Value, String> {
    let (operation_config, _notification_side_effects) =
        split_action_config(&action.config).map_err(|error| error.to_string())?;

    let invocation = match resolve_inline_function_config(state, &operation_config).await {
        Ok(Some(_)) => {
            return Err(
                "batched_execution requires an HTTP-backed function invocation".to_string(),
            );
        }
        Ok(None) => validate_http_invocation_config(&operation_config)?,
        Err(error) => return Err(error),
    };

    let batch_items: Vec<Value> = target_object_ids
        .iter()
        .map(|object_id| {
            let mut entry = serde_json::Map::new();
            entry.insert("target_object_id".to_string(), json!(object_id));
            if let Some(map) = parameters.as_object() {
                for (key, value) in map {
                    entry.insert(key.clone(), value.clone());
                }
            }
            Value::Object(entry)
        })
        .collect();

    let payload = json!({
        "action": {
            "id": action.id,
            "name": &action.name,
            "display_name": &action.display_name,
            "object_type_id": action.object_type_id,
            "operation_kind": &action.operation_kind,
        },
        "actor": claims.sub,
        "justification": justification,
        "batched": true,
        "parameters": {
            "batch": batch_items,
        },
    });

    invoke_http_action(state, &invocation, &payload).await
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body;
    use chrono::Utc;
    use storage_abstraction::repositories::{
        DefinitionId, DefinitionKind, DefinitionRecord, DefinitionStore, ObjectStore, RepoResult,
    };

    fn sample_input_schema() -> Vec<ActionInputField> {
        vec![
            ActionInputField {
                name: "mode".to_string(),
                display_name: Some("Mode".to_string()),
                description: None,
                property_type: "string".to_string(),
                required: true,
                default_value: None,
                struct_fields: None,
            },
            ActionInputField {
                name: "reason".to_string(),
                display_name: Some("Reason".to_string()),
                description: None,
                property_type: "string".to_string(),
                required: false,
                default_value: None,
                struct_fields: None,
            },
            ActionInputField {
                name: "owner".to_string(),
                display_name: Some("Owner".to_string()),
                description: None,
                property_type: "string".to_string(),
                required: false,
                default_value: None,
                struct_fields: None,
            },
        ]
    }

    fn sample_object() -> ObjectInstance {
        ObjectInstance {
            id: Uuid::now_v7(),
            object_type_id: Uuid::now_v7(),
            properties: json!({
                "assignee": Uuid::nil().to_string(),
                "watchers": [Uuid::max().to_string()],
                "title": "Example ticket",
            }),
            created_by: Uuid::from_u128(7),
            organization_id: None,
            marking: "public".to_string(),
            created_at: Utc::now(),
            updated_at: Utc::now(),
        }
    }

    fn test_claims(user_id: Uuid, org_id: Uuid) -> Claims {
        let now = Utc::now().timestamp();
        Claims {
            sub: user_id,
            iat: now,
            exp: now + 3600,
            iss: None,
            aud: None,
            jti: Uuid::now_v7(),
            email: "actions-test@openfoundry.dev".to_string(),
            name: "Actions Test".to_string(),
            roles: vec!["admin".to_string()],
            permissions: vec!["*:*".to_string()],
            org_id: Some(org_id),
            attributes: json!({
                "classification_clearance": "public"
            }),
            auth_methods: vec!["password".to_string()],
            token_use: Some("access".to_string()),
            api_key_id: None,
            session_kind: None,
            session_scope: None,
        }
    }

    fn test_state() -> AppState {
        AppState {
            db: crate::test_support::lazy_pg_pool(),
            stores: crate::stores::Stores::in_memory(),
            http_client: reqwest::Client::new(),
            jwt_config: auth_middleware::jwt::JwtConfig::new("actions-test-secret"),
            audit_service_url: String::new(),
            dataset_service_url: String::new(),
            ontology_service_url: String::new(),
            pipeline_service_url: String::new(),
            ai_service_url: String::new(),
            notification_service_url: String::new(),
            search_embedding_provider: "deterministic".to_string(),
            node_runtime_command: "node".to_string(),
            connector_management_service_url: String::new(),
        }
    }

    async fn response_json(response: Response) -> Value {
        let bytes = body::to_bytes(response.into_body(), 1024 * 1024)
            .await
            .expect("body bytes");
        if bytes.is_empty() {
            Value::Null
        } else {
            serde_json::from_slice(&bytes).expect("json body")
        }
    }

    async fn seed_object_type(
        store: &dyn DefinitionStore,
        object_type_id: Uuid,
        owner_id: Uuid,
    ) -> RepoResult<()> {
        let now = Utc::now();
        store
            .put(
                DefinitionRecord {
                    kind: DefinitionKind(action_repository::OBJECT_TYPE_KIND.to_string()),
                    id: DefinitionId(object_type_id.to_string()),
                    tenant: None,
                    owner_id: Some(owner_id.to_string()),
                    parent_id: None,
                    version: Some(1),
                    payload: json!({
                        "id": object_type_id,
                        "name": "ticket",
                        "display_name": "Ticket",
                        "description": "Ticket",
                        "primary_key_property": "title",
                        "owner_id": owner_id,
                        "created_at": now,
                        "updated_at": now,
                    }),
                    created_at_ms: Some(now.timestamp_millis()),
                    updated_at_ms: Some(now.timestamp_millis()),
                },
                None,
            )
            .await
            .map(|_| ())
    }

    async fn seed_delete_action(
        store: &dyn DefinitionStore,
        action_id: Uuid,
        object_type_id: Uuid,
        owner_id: Uuid,
    ) -> RepoResult<ActionType> {
        let now = Utc::now();
        let action = ActionType {
            id: action_id,
            name: "delete_ticket".to_string(),
            display_name: "Delete ticket".to_string(),
            description: "Delete a ticket".to_string(),
            object_type_id,
            operation_kind: "delete_object".to_string(),
            input_schema: Vec::new(),
            form_schema: ActionFormSchema::default(),
            config: Value::Null,
            confirmation_required: false,
            permission_key: None,
            authorization_policy: ActionAuthorizationPolicy::default(),
            owner_id,
            created_at: now,
            updated_at: now,
        };
        action_repository::put_action(store, action.clone())
            .await
            .map(|_| action)
    }

    async fn seed_object(
        state: &AppState,
        claims: &Claims,
        object_id: Uuid,
        object_type_id: Uuid,
    ) -> RepoResult<()> {
        let now_ms = Utc::now().timestamp_millis();
        state
            .stores
            .objects
            .put(
                Object {
                    tenant: tenant_from_claims(claims),
                    id: ObjectId(object_id.to_string()),
                    type_id: TypeId(object_type_id.to_string()),
                    version: 1,
                    payload: json!({ "title": "T-1", "status": "open" }),
                    organization_id: claims.org_id.map(|id| id.to_string()),
                    created_at_ms: Some(now_ms),
                    updated_at_ms: now_ms,
                    owner: Some(OwnerId(claims.sub.to_string())),
                    markings: vec![MarkingId("public".to_string())],
                },
                None,
            )
            .await
            .map(|_| ())
    }

    #[tokio::test]
    async fn execute_action_uses_stores_and_records_action_log() {
        let state = test_state();
        let user_id = Uuid::now_v7();
        let org_id = Uuid::now_v7();
        let claims = test_claims(user_id, org_id);
        let object_type_id = Uuid::now_v7();
        let object_id = Uuid::now_v7();
        let action_id = Uuid::now_v7();

        seed_object_type(state.stores.definitions.as_ref(), object_type_id, user_id)
            .await
            .expect("object type");
        seed_delete_action(
            state.stores.definitions.as_ref(),
            action_id,
            object_type_id,
            user_id,
        )
        .await
        .expect("action");
        seed_object(&state, &claims, object_id, object_type_id)
            .await
            .expect("object");

        let response = execute_action(
            AuthUser(claims.clone()),
            State(state.clone()),
            Path(action_id),
            Json(ExecuteActionRequest {
                target_object_id: Some(object_id),
                parameters: json!({}),
                justification: None,
            }),
        )
        .await
        .into_response();

        assert_eq!(response.status(), StatusCode::OK);
        let payload = response_json(response).await;
        assert_eq!(payload["deleted"], json!(true));
        assert_eq!(payload["target_object_id"], json!(object_id.to_string()));

        let deleted = state
            .stores
            .objects
            .get(
                &tenant_from_claims(&claims),
                &ObjectId(object_id.to_string()),
                ReadConsistency::Strong,
            )
            .await
            .expect("load object");
        assert!(deleted.is_none());

        let log = state
            .stores
            .actions
            .list_for_action(
                &tenant_from_claims(&claims),
                &action_id.to_string(),
                Page {
                    size: 10,
                    token: None,
                },
                ReadConsistency::Strong,
            )
            .await
            .expect("action log");
        assert_eq!(log.items.len(), 1);
        assert_eq!(log.items[0].kind, "action_attempt");
        assert_eq!(log.items[0].payload["status"], json!("success"));
        assert!(log.items[0].event_id.is_some());
    }

    #[tokio::test]
    async fn action_log_append_is_idempotent_for_retry() {
        let state = test_state();
        let claims = test_claims(Uuid::now_v7(), Uuid::now_v7());
        let action_id = Uuid::now_v7();
        let object_id = Uuid::now_v7();
        let parameters = json!({ "status": "closed" });

        persist_action_attempt_row(
            &state,
            &claims,
            action_id,
            Some(object_id),
            &parameters,
            "success",
            None,
            42,
        )
        .await
        .expect("first append");
        persist_action_attempt_row(
            &state,
            &claims,
            action_id,
            Some(object_id),
            &parameters,
            "success",
            None,
            42,
        )
        .await
        .expect("retry append");

        let page = state
            .stores
            .actions
            .list_for_action(
                &tenant_from_claims(&claims),
                &action_id.to_string(),
                Page {
                    size: 10,
                    token: None,
                },
                ReadConsistency::Strong,
            )
            .await
            .expect("action log");
        assert_eq!(page.items.len(), 1);
    }

    #[tokio::test]
    async fn side_effect_event_log_is_idempotent() {
        let state = test_state();
        let claims = test_claims(Uuid::now_v7(), Uuid::now_v7());
        let action_id = Uuid::now_v7();
        let webhook_id = Uuid::now_v7();

        persist_webhook_side_effect_row(
            &state,
            &claims,
            action_id,
            None,
            webhook_id,
            claims.sub,
            "failure",
            None,
            Some("network"),
        )
        .await;
        persist_webhook_side_effect_row(
            &state,
            &claims,
            action_id,
            None,
            webhook_id,
            claims.sub,
            "failure",
            None,
            Some("network"),
        )
        .await;

        let page = state
            .stores
            .actions
            .list_for_action(
                &tenant_from_claims(&claims),
                &action_id.to_string(),
                Page {
                    size: 10,
                    token: None,
                },
                ReadConsistency::Strong,
            )
            .await
            .expect("action log");
        assert_eq!(page.items.len(), 1);
        assert_eq!(page.items[0].kind, "side_effect");
    }

    #[test]
    fn writeback_outbox_event_ids_are_deterministic() {
        let first =
            writeback::derive_event_id("tenant-a", "object", "object-1", 7);
        let retry =
            writeback::derive_event_id("tenant-a", "object", "object-1", 7);
        let next_version =
            writeback::derive_event_id("tenant-a", "object", "object-1", 8);

        assert_eq!(first, retry);
        assert_ne!(first, next_version);
    }

    #[test]
    fn split_action_config_supports_legacy_and_envelope() {
        let legacy = json!({ "url": "http://localhost:1234/hook" });
        let (operation, notifications) = split_action_config(&legacy).expect("legacy config");
        assert_eq!(operation, legacy);
        assert!(notifications.is_empty());

        let envelope = json!({
            "operation": { "url": "http://localhost:1234/hook" },
            "notification_side_effects": [
                {
                    "title": "Done",
                    "body": "Action completed",
                    "send_to_actor": true
                }
            ]
        });
        let (operation, notifications) = split_action_config(&envelope).expect("envelope config");
        assert_eq!(operation, json!({ "url": "http://localhost:1234/hook" }));
        assert_eq!(notifications.len(), 1);
        assert!(notifications[0].send_to_actor);
    }

    #[test]
    fn render_template_resolves_nested_context() {
        let context = json!({
            "action": { "display_name": "Escalate ticket" },
            "parameters": { "priority": "P1" },
            "target": { "properties": { "title": "Example ticket" } }
        });

        let rendered = render_template(
            "Action {{action.display_name}} set {{target.properties.title}} to {{parameters.priority}}",
            &context,
        );

        assert_eq!(rendered, "Action Escalate ticket set Example ticket to P1");
    }

    #[test]
    fn resolve_notification_recipients_combines_sources() {
        let target = sample_object();
        let config: ActionNotificationSideEffectConfig = serde_json::from_value(json!({
            "title": "Done",
            "body": "Body",
            "user_ids": [Uuid::from_u128(10).to_string()],
            "user_id_input_name": "reviewers",
            "target_user_property_name": "watchers",
            "send_to_actor": true,
            "send_to_target_creator": true
        }))
        .expect("notification config");

        let parameters = HashMap::from([(
            "reviewers".to_string(),
            json!([
                Uuid::from_u128(11).to_string(),
                Uuid::from_u128(12).to_string()
            ]),
        )]);

        let (recipients, broadcast) = resolve_notification_recipients(
            &config,
            &parameters,
            Some(&target),
            Uuid::from_u128(13),
        )
        .expect("recipients");

        assert!(!broadcast);
        assert_eq!(
            recipients,
            vec![
                Uuid::from_u128(7),
                Uuid::from_u128(10),
                Uuid::from_u128(11),
                Uuid::from_u128(12),
                Uuid::from_u128(13),
                Uuid::max(),
            ]
        );
    }

    #[test]
    fn materialize_parameters_applies_form_overrides_and_defaults() {
        let mut target = sample_object();
        target.marking = "sensitive".to_string();

        let form_schema: ActionFormSchema = serde_json::from_value(json!({
            "parameter_overrides": [
                {
                    "parameter_name": "reason",
                    "conditions": [
                        { "left": "parameters.mode", "operator": "is", "right": "manual" }
                    ],
                    "required": true
                },
                {
                    "parameter_name": "owner",
                    "conditions": [
                        { "left": "target.marking", "operator": "is", "right": "sensitive" }
                    ],
                    "default_value": "ops"
                }
            ]
        }))
        .expect("form schema");

        let missing_reason = materialize_parameters(
            &sample_input_schema(),
            &json!({ "mode": "manual" }),
            &form_schema,
            Some(&target),
        )
        .expect_err("reason should become required");
        assert_eq!(missing_reason, vec!["reason is required"]);

        let parameters = materialize_parameters(
            &sample_input_schema(),
            &json!({ "mode": "manual", "reason": "escalation" }),
            &form_schema,
            Some(&target),
        )
        .expect("parameters");

        assert_eq!(parameters.get("owner"), Some(&json!("ops")));
        assert_eq!(parameters.get("reason"), Some(&json!("escalation")));
    }

    #[test]
    fn validate_action_form_schema_rejects_unknown_parameter_paths() {
        let input_schema = sample_input_schema();
        let property_types = HashMap::from([("status", "string")]);
        let form_schema: ActionFormSchema = serde_json::from_value(json!({
            "sections": [
                {
                    "id": "main",
                    "columns": 2,
                    "parameter_names": ["mode"]
                }
            ],
            "parameter_overrides": [
                {
                    "parameter_name": "reason",
                    "conditions": [
                        { "left": "parameters.missing", "operator": "is", "right": "yes" }
                    ],
                    "required": true
                }
            ]
        }))
        .expect("form schema");

        let error = validate_action_form_schema(&form_schema, &input_schema, &property_types)
            .expect_err("invalid condition path");
        assert!(error.contains("unknown parameter path"));
    }
}
