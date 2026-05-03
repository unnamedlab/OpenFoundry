//! Repository boundary for saved object set definitions.
//!
//! Object set definitions are declarative control-plane metadata, while
//! evaluation/runtime rows are loaded through search/read-model stores. This
//! module keeps handlers free of inline SQL and talks only to
//! [`storage_abstraction::repositories::DefinitionStore`].

use chrono::{DateTime, Utc};
use serde_json::Value;
use storage_abstraction::repositories::{
    DefinitionId, DefinitionKind, DefinitionQuery, DefinitionRecord, DefinitionStore, Page,
    PagedResult, PutOutcome, ReadConsistency, RepoError, RepoResult,
};
use uuid::Uuid;

use crate::models::object_set::ObjectSetDefinition;

pub const KIND: &str = "object_set";
pub const OBJECT_TYPE_KIND: &str = "object_type";

#[derive(Debug, Clone)]
pub struct ObjectSetListQuery {
    pub owner_id: Uuid,
    pub include_restricted_views: bool,
    pub page: Page,
}

fn kind() -> DefinitionKind {
    DefinitionKind(KIND.to_string())
}

fn object_type_kind() -> DefinitionKind {
    DefinitionKind(OBJECT_TYPE_KIND.to_string())
}

fn definition_id(id: Uuid) -> DefinitionId {
    DefinitionId(id.to_string())
}

fn dt_to_ms(value: DateTime<Utc>) -> i64 {
    value.timestamp_millis()
}

fn parse_uuid(payload: &Value, field: &str) -> RepoResult<Uuid> {
    payload
        .get(field)
        .and_then(Value::as_str)
        .ok_or_else(|| RepoError::Backend(format!("object set missing {field}")))?
        .parse::<Uuid>()
        .map_err(|error| RepoError::Backend(format!("invalid object set {field}: {error}")))
}

fn parse_dt(payload: &Value, field: &str) -> RepoResult<DateTime<Utc>> {
    let raw = payload
        .get(field)
        .and_then(Value::as_str)
        .ok_or_else(|| RepoError::Backend(format!("object set missing {field}")))?;
    DateTime::parse_from_rfc3339(raw)
        .map(|value| value.with_timezone(&Utc))
        .map_err(|error| RepoError::Backend(format!("invalid object set {field}: {error}")))
}

fn parse_json<T>(payload: &Value, field: &str) -> RepoResult<T>
where
    T: serde::de::DeserializeOwned,
{
    serde_json::from_value(payload.get(field).cloned().unwrap_or(Value::Null))
        .map_err(|error| RepoError::Backend(format!("invalid object set {field}: {error}")))
}

pub fn definition_to_record(definition: &ObjectSetDefinition) -> RepoResult<DefinitionRecord> {
    let payload = serde_json::to_value(definition)
        .map_err(|error| RepoError::Backend(format!("object set serialize failed: {error}")))?;
    Ok(DefinitionRecord {
        kind: kind(),
        id: definition_id(definition.id),
        tenant: None,
        owner_id: Some(definition.owner_id.to_string()),
        parent_id: Some(DefinitionId(definition.base_object_type_id.to_string())),
        version: Some(definition.updated_at.timestamp_millis() as u64),
        payload,
        created_at_ms: Some(dt_to_ms(definition.created_at)),
        updated_at_ms: Some(dt_to_ms(definition.updated_at)),
    })
}

pub fn definition_from_record(record: DefinitionRecord) -> RepoResult<ObjectSetDefinition> {
    let payload = record.payload;
    let join = payload
        .get("join")
        .cloned()
        .or_else(|| payload.get("join_config").cloned())
        .filter(|value| !value.is_null());

    Ok(ObjectSetDefinition {
        id: parse_uuid(&payload, "id")?,
        name: payload
            .get("name")
            .and_then(Value::as_str)
            .unwrap_or_default()
            .to_string(),
        description: payload
            .get("description")
            .and_then(Value::as_str)
            .unwrap_or_default()
            .to_string(),
        base_object_type_id: parse_uuid(&payload, "base_object_type_id")?,
        filters: parse_json(&payload, "filters")?,
        traversals: parse_json(&payload, "traversals")?,
        join: match join {
            Some(value) => Some(serde_json::from_value(value).map_err(|error| {
                RepoError::Backend(format!("invalid object set join_config: {error}"))
            })?),
            None => None,
        },
        projections: parse_json(&payload, "projections")?,
        what_if_label: payload
            .get("what_if_label")
            .and_then(Value::as_str)
            .map(ToOwned::to_owned),
        policy: parse_json(&payload, "policy")?,
        materialized_snapshot: payload.get("materialized_snapshot").cloned(),
        materialized_at: payload
            .get("materialized_at")
            .and_then(Value::as_str)
            .and_then(|raw| DateTime::parse_from_rfc3339(raw).ok())
            .map(|value| value.with_timezone(&Utc)),
        materialized_row_count: payload
            .get("materialized_row_count")
            .and_then(Value::as_i64)
            .and_then(|value| i32::try_from(value).ok())
            .unwrap_or_default(),
        owner_id: parse_uuid(&payload, "owner_id")?,
        created_at: parse_dt(&payload, "created_at")?,
        updated_at: parse_dt(&payload, "updated_at")?,
    })
}

pub async fn get(store: &dyn DefinitionStore, id: Uuid) -> RepoResult<Option<ObjectSetDefinition>> {
    store
        .get(&kind(), &definition_id(id), ReadConsistency::Strong)
        .await?
        .map(definition_from_record)
        .transpose()
}

pub async fn list(
    store: &dyn DefinitionStore,
    query: ObjectSetListQuery,
) -> RepoResult<PagedResult<ObjectSetDefinition>> {
    let mut filters = std::collections::HashMap::new();
    if !query.include_restricted_views {
        filters.insert("owner_id".to_string(), query.owner_id.to_string());
    }
    let page = store
        .list(
            DefinitionQuery {
                kind: kind(),
                tenant: None,
                owner_id: None,
                parent_id: None,
                search: None,
                filters,
                page: query.page,
            },
            ReadConsistency::Strong,
        )
        .await?;

    let mut items = Vec::with_capacity(page.items.len());
    for record in page.items {
        let definition = definition_from_record(record)?;
        if definition.owner_id == query.owner_id
            || (query.include_restricted_views
                && definition.policy.required_restricted_view_id.is_some())
        {
            items.push(definition);
        }
    }

    Ok(PagedResult {
        items,
        next_token: page.next_token,
    })
}

pub async fn create(
    store: &dyn DefinitionStore,
    definition: ObjectSetDefinition,
) -> RepoResult<PutOutcome> {
    store.put(definition_to_record(&definition)?, None).await
}

pub async fn update(
    store: &dyn DefinitionStore,
    definition: ObjectSetDefinition,
) -> RepoResult<PutOutcome> {
    store.put(definition_to_record(&definition)?, None).await
}

pub async fn delete(store: &dyn DefinitionStore, id: Uuid) -> RepoResult<bool> {
    store.delete(&kind(), &definition_id(id)).await
}

pub async fn object_type_exists(
    store: &dyn DefinitionStore,
    object_type_id: Uuid,
) -> RepoResult<bool> {
    store
        .get(
            &object_type_kind(),
            &definition_id(object_type_id),
            ReadConsistency::Strong,
        )
        .await
        .map(|record| record.is_some())
}
