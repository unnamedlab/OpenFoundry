//! Repository boundary for ontology action definitions and warm action views.
//!
//! Action definitions are declarative metadata; execution attempts, side
//! effects and audit events live in [`storage_abstraction::repositories::ActionLogStore`].
//! This module keeps HTTP handlers away from direct SQL while preserving the
//! existing public action models.

use std::collections::HashMap;

use chrono::{DateTime, Utc};
use serde_json::{Value, json};
use storage_abstraction::repositories::{
    DefinitionId, DefinitionKind, DefinitionQuery, DefinitionRecord, DefinitionStore, Page,
    PagedResult, PutOutcome, ReadConsistency, ReadModelId, ReadModelKind, ReadModelQuery,
    ReadModelRecord, ReadModelStore, RepoError, RepoResult, TenantId,
};
use uuid::Uuid;

use crate::{
    domain::schema::EffectivePropertyDefinition,
    models::{
        action_type::{ActionType, ActionTypeRow, ActionWhatIfBranch},
        link_type::LinkType,
        property::Property,
    },
};

pub const ACTION_TYPE_KIND: &str = "action_type";
pub const OBJECT_TYPE_KIND: &str = "object_type";
pub const PROPERTY_KIND: &str = "property";
pub const LINK_TYPE_KIND: &str = "link_type";
pub const WHAT_IF_KIND: &str = "action_what_if_branch";

#[derive(Debug, Clone)]
pub struct ActionTypeListQuery {
    pub object_type_id: Option<Uuid>,
    pub search: Option<String>,
    pub page: Page,
}

#[derive(Debug, Clone)]
pub struct WhatIfListQuery {
    pub tenant: TenantId,
    pub action_id: Uuid,
    pub target_object_id: Option<Uuid>,
    pub owner_id: Uuid,
    pub show_all: bool,
    pub page: Page,
}

fn kind(name: &str) -> DefinitionKind {
    DefinitionKind(name.to_string())
}

fn id(value: Uuid) -> DefinitionId {
    DefinitionId(value.to_string())
}

fn read_model_kind(name: &str) -> ReadModelKind {
    ReadModelKind(name.to_string())
}

fn read_model_id(value: Uuid) -> ReadModelId {
    ReadModelId(value.to_string())
}

fn parse_dt(payload: &Value, field: &str) -> RepoResult<DateTime<Utc>> {
    let raw = payload
        .get(field)
        .and_then(Value::as_str)
        .ok_or_else(|| RepoError::Backend(format!("definition payload missing {field}")))?;
    DateTime::parse_from_rfc3339(raw)
        .map(|value| value.with_timezone(&Utc))
        .map_err(|error| RepoError::Backend(format!("invalid {field}: {error}")))
}

fn parse_uuid(payload: &Value, field: &str) -> RepoResult<Uuid> {
    payload
        .get(field)
        .and_then(Value::as_str)
        .ok_or_else(|| RepoError::Backend(format!("definition payload missing {field}")))?
        .parse()
        .map_err(|error| RepoError::Backend(format!("invalid {field}: {error}")))
}

fn payload_bool(payload: &Value, field: &str) -> bool {
    payload
        .get(field)
        .and_then(Value::as_bool)
        .unwrap_or_default()
}

fn payload_string(payload: &Value, field: &str) -> String {
    payload
        .get(field)
        .and_then(Value::as_str)
        .unwrap_or_default()
        .to_string()
}

pub fn action_to_record(action: &ActionType) -> RepoResult<DefinitionRecord> {
    let payload = serde_json::to_value(action)
        .map_err(|error| RepoError::Backend(format!("action type serialize failed: {error}")))?;
    Ok(DefinitionRecord {
        kind: kind(ACTION_TYPE_KIND),
        id: id(action.id),
        tenant: None,
        owner_id: Some(action.owner_id.to_string()),
        parent_id: Some(id(action.object_type_id)),
        version: Some(action.updated_at.timestamp_millis() as u64),
        payload,
        created_at_ms: Some(action.created_at.timestamp_millis()),
        updated_at_ms: Some(action.updated_at.timestamp_millis()),
    })
}

pub fn row_from_record(record: DefinitionRecord) -> RepoResult<ActionTypeRow> {
    let payload = record.payload;
    Ok(ActionTypeRow {
        id: parse_uuid(&payload, "id")?,
        name: payload_string(&payload, "name"),
        display_name: payload_string(&payload, "display_name"),
        description: payload_string(&payload, "description"),
        object_type_id: parse_uuid(&payload, "object_type_id")?,
        operation_kind: payload_string(&payload, "operation_kind"),
        input_schema: payload
            .get("input_schema")
            .cloned()
            .unwrap_or_else(|| json!([])),
        form_schema: payload
            .get("form_schema")
            .cloned()
            .unwrap_or_else(|| json!({})),
        config: payload.get("config").cloned().unwrap_or(Value::Null),
        confirmation_required: payload_bool(&payload, "confirmation_required"),
        permission_key: payload
            .get("permission_key")
            .and_then(Value::as_str)
            .map(ToOwned::to_owned),
        authorization_policy: payload
            .get("authorization_policy")
            .cloned()
            .unwrap_or_else(|| json!({})),
        owner_id: parse_uuid(&payload, "owner_id")?,
        created_at: parse_dt(&payload, "created_at")?,
        updated_at: parse_dt(&payload, "updated_at")?,
    })
}

pub async fn get_action_row(
    store: &dyn DefinitionStore,
    action_id: Uuid,
) -> RepoResult<Option<ActionTypeRow>> {
    store
        .get(
            &kind(ACTION_TYPE_KIND),
            &id(action_id),
            ReadConsistency::Strong,
        )
        .await?
        .map(row_from_record)
        .transpose()
}

pub async fn list_action_rows(
    store: &dyn DefinitionStore,
    query: ActionTypeListQuery,
) -> RepoResult<PagedResult<ActionTypeRow>> {
    let page = store
        .list(
            DefinitionQuery {
                kind: kind(ACTION_TYPE_KIND),
                tenant: None,
                owner_id: None,
                parent_id: query.object_type_id.map(id),
                search: query.search,
                filters: HashMap::new(),
                page: query.page,
            },
            ReadConsistency::Strong,
        )
        .await?;
    let items = page
        .items
        .into_iter()
        .map(row_from_record)
        .collect::<RepoResult<Vec<_>>>()?;
    Ok(PagedResult {
        items,
        next_token: page.next_token,
    })
}

pub async fn count_action_rows(
    store: &dyn DefinitionStore,
    object_type_id: Option<Uuid>,
    search: Option<String>,
) -> RepoResult<u64> {
    store
        .count(
            DefinitionQuery {
                kind: kind(ACTION_TYPE_KIND),
                tenant: None,
                owner_id: None,
                parent_id: object_type_id.map(id),
                search,
                filters: HashMap::new(),
                page: Page {
                    size: 1,
                    token: None,
                },
            },
            ReadConsistency::Strong,
        )
        .await
}

pub async fn put_action(store: &dyn DefinitionStore, action: ActionType) -> RepoResult<PutOutcome> {
    store.put(action_to_record(&action)?, None).await
}

pub async fn delete_action(store: &dyn DefinitionStore, action_id: Uuid) -> RepoResult<bool> {
    store.delete(&kind(ACTION_TYPE_KIND), &id(action_id)).await
}

pub async fn object_type_exists(
    store: &dyn DefinitionStore,
    object_type_id: Uuid,
) -> RepoResult<bool> {
    store
        .get(
            &kind(OBJECT_TYPE_KIND),
            &id(object_type_id),
            ReadConsistency::Strong,
        )
        .await
        .map(|record| record.is_some())
}

pub async fn load_property_for_object_type(
    store: &dyn DefinitionStore,
    object_type_id: Uuid,
    property_id: Uuid,
) -> RepoResult<Option<Property>> {
    let Some(record) = store
        .get(
            &kind(PROPERTY_KIND),
            &id(property_id),
            ReadConsistency::Strong,
        )
        .await?
    else {
        return Ok(None);
    };
    let property = serde_json::from_value::<Property>(record.payload)
        .map_err(|error| RepoError::Backend(format!("invalid property definition: {error}")))?;
    Ok((property.object_type_id == object_type_id).then_some(property))
}

pub async fn load_effective_properties(
    store: &dyn DefinitionStore,
    object_type_id: Uuid,
) -> RepoResult<Vec<EffectivePropertyDefinition>> {
    let page = store
        .list(
            DefinitionQuery {
                kind: kind(PROPERTY_KIND),
                tenant: None,
                owner_id: None,
                parent_id: Some(id(object_type_id)),
                search: None,
                filters: HashMap::new(),
                page: Page {
                    size: 1_000,
                    token: None,
                },
            },
            ReadConsistency::Strong,
        )
        .await?;
    page.items
        .into_iter()
        .map(|record| {
            let property = serde_json::from_value::<Property>(record.payload).map_err(|error| {
                RepoError::Backend(format!("invalid property definition: {error}"))
            })?;
            Ok(EffectivePropertyDefinition {
                name: property.name,
                display_name: property.display_name,
                description: property.description,
                property_type: property.property_type,
                required: property.required,
                unique_constraint: property.unique_constraint,
                time_dependent: property.time_dependent,
                default_value: property.default_value,
                validation_rules: property.validation_rules,
                source: "object_type".to_string(),
            })
        })
        .collect()
}

pub async fn load_link_type(
    store: &dyn DefinitionStore,
    link_type_id: Uuid,
) -> RepoResult<Option<LinkType>> {
    store
        .get(
            &kind(LINK_TYPE_KIND),
            &id(link_type_id),
            ReadConsistency::Strong,
        )
        .await?
        .map(|record| {
            serde_json::from_value::<LinkType>(record.payload).map_err(|error| {
                RepoError::Backend(format!("invalid link type definition: {error}"))
            })
        })
        .transpose()
}

pub async fn action_has_inline_edit_references(
    store: &dyn DefinitionStore,
    action_id: Uuid,
) -> RepoResult<bool> {
    let page = store
        .list(
            DefinitionQuery {
                kind: kind(PROPERTY_KIND),
                tenant: None,
                owner_id: None,
                parent_id: None,
                search: None,
                filters: HashMap::new(),
                page: Page {
                    size: 10_000,
                    token: None,
                },
            },
            ReadConsistency::Strong,
        )
        .await?;
    Ok(page.items.into_iter().any(|record| {
        record
            .payload
            .get("inline_edit_config")
            .and_then(|value| value.get("action_type_id"))
            .and_then(Value::as_str)
            .is_some_and(|value| value == action_id.to_string())
    }))
}

pub async fn create_what_if_branch(
    store: &dyn ReadModelStore,
    tenant: TenantId,
    branch: ActionWhatIfBranch,
) -> RepoResult<ActionWhatIfBranch> {
    store
        .put(ReadModelRecord {
            kind: read_model_kind(WHAT_IF_KIND),
            tenant,
            id: read_model_id(branch.id),
            parent_id: Some(read_model_id(branch.action_id)),
            payload: serde_json::to_value(&branch).map_err(|error| {
                RepoError::Backend(format!("what-if branch serialize failed: {error}"))
            })?,
            version: branch.updated_at.timestamp_millis() as u64,
            updated_at_ms: branch.updated_at.timestamp_millis(),
        })
        .await?;
    Ok(branch)
}

pub async fn list_what_if_branches(
    store: &dyn ReadModelStore,
    query: WhatIfListQuery,
) -> RepoResult<PagedResult<ActionWhatIfBranch>> {
    let mut filters = HashMap::new();
    if let Some(target_object_id) = query.target_object_id {
        filters.insert("target_object_id".to_string(), target_object_id.to_string());
    }
    if !query.show_all {
        filters.insert("owner_id".to_string(), query.owner_id.to_string());
    }
    let page = store
        .list(
            ReadModelQuery {
                kind: read_model_kind(WHAT_IF_KIND),
                tenant: query.tenant,
                parent_id: Some(read_model_id(query.action_id)),
                filters,
                page: query.page,
            },
            ReadConsistency::Strong,
        )
        .await?;
    let items = page
        .items
        .into_iter()
        .map(|record| {
            serde_json::from_value::<ActionWhatIfBranch>(record.payload).map_err(|error| {
                RepoError::Backend(format!("invalid what-if branch read model: {error}"))
            })
        })
        .collect::<RepoResult<Vec<_>>>()?;
    Ok(PagedResult {
        items,
        next_token: page.next_token,
    })
}

pub async fn count_what_if_branches(
    store: &dyn ReadModelStore,
    query: WhatIfListQuery,
) -> RepoResult<u64> {
    Ok(list_what_if_branches(store, query).await?.items.len() as u64)
}

pub async fn delete_what_if_branch(
    store: &dyn ReadModelStore,
    tenant: &TenantId,
    action_id: Uuid,
    branch_id: Uuid,
    owner_id: Uuid,
    show_all: bool,
) -> RepoResult<bool> {
    let kind = read_model_kind(WHAT_IF_KIND);
    let Some(record) = store
        .get(
            &kind,
            tenant,
            &read_model_id(branch_id),
            ReadConsistency::Strong,
        )
        .await?
    else {
        return Ok(false);
    };
    let branch = serde_json::from_value::<ActionWhatIfBranch>(record.payload).map_err(|error| {
        RepoError::Backend(format!("invalid what-if branch read model: {error}"))
    })?;
    if branch.action_id != action_id || (!show_all && branch.owner_id != owner_id) {
        return Ok(false);
    }
    store.delete(&kind, tenant, &read_model_id(branch_id)).await
}
