use std::collections::HashMap;

use auth_middleware::claims::Claims;
use chrono::{DateTime, TimeZone, Utc};
use serde_json::Value;
use storage_abstraction::repositories::{
    Object, ObjectId, Page, ReadConsistency, SearchHit, SearchQuery, TenantId, TypeId,
};
use uuid::Uuid;

use crate::{
    AppState, domain::access::ensure_object_access, domain::function_runtime::object_to_json,
    handlers::objects::ObjectInstance,
};

pub fn tenant_from_claims(claims: &Claims) -> TenantId {
    TenantId(
        claims
            .org_id
            .map(|id| id.to_string())
            .unwrap_or_else(|| "default".to_string()),
    )
}

pub fn search_hit_to_object_instance(
    hit: &SearchHit,
    fallback_org_id: Option<Uuid>,
) -> Option<ObjectInstance> {
    let snippet = hit.snippet.as_ref()?;
    let id = parse_uuid(snippet.get("id")).or_else(|| Uuid::parse_str(&hit.id.0).ok())?;
    let object_type_id = parse_uuid(snippet.get("object_type_id"))
        .or_else(|| parse_uuid(snippet.get("type_id")))
        .or_else(|| Uuid::parse_str(&hit.type_id.0).ok())?;
    let properties = snippet
        .get("properties")
        .cloned()
        .or_else(|| snippet.get("payload").cloned())
        .unwrap_or(Value::Object(Default::default()));
    let created_by = parse_uuid(snippet.get("created_by")).unwrap_or_else(Uuid::nil);
    let organization_id = parse_uuid(snippet.get("organization_id")).or(fallback_org_id);
    let marking = snippet
        .get("marking")
        .and_then(Value::as_str)
        .map(str::to_string)
        .or_else(|| {
            snippet
                .get("markings")
                .and_then(Value::as_array)
                .and_then(|markings| markings.first())
                .and_then(Value::as_str)
                .map(str::to_string)
        })
        .unwrap_or_else(|| "public".to_string());
    let updated_at = parse_datetime(snippet.get("updated_at")).unwrap_or_else(Utc::now);
    let created_at = parse_datetime(snippet.get("created_at")).unwrap_or(updated_at);

    Some(ObjectInstance {
        id,
        object_type_id,
        properties,
        created_by,
        organization_id,
        marking,
        created_at,
        updated_at,
    })
}

pub fn object_store_to_object_instance(
    object: Object,
    fallback_org_id: Option<Uuid>,
) -> Option<ObjectInstance> {
    let id = Uuid::parse_str(&object.id.0).ok()?;
    let object_type_id = Uuid::parse_str(&object.type_id.0).ok()?;
    let timestamp = Utc.timestamp_millis_opt(object.updated_at_ms).single()?;
    let created_by = object
        .owner
        .as_ref()
        .and_then(|owner| Uuid::parse_str(&owner.0).ok())
        .unwrap_or_else(Uuid::nil);
    let marking = object
        .markings
        .first()
        .map(|marking| marking.0.clone())
        .unwrap_or_else(|| "public".to_string());

    Some(ObjectInstance {
        id,
        object_type_id,
        properties: object.payload,
        created_by,
        organization_id: fallback_org_id,
        marking,
        created_at: timestamp,
        updated_at: timestamp,
    })
}

pub async fn load_object_instance_from_read_model(
    state: &AppState,
    claims: &Claims,
    object_id: Uuid,
    object_type_id: Option<Uuid>,
) -> Result<Option<ObjectInstance>, String> {
    let tenant = tenant_from_claims(claims);
    let type_filter = object_type_id.map(|value| TypeId(value.to_string()));
    let hits = state
        .stores
        .search
        .search(
            SearchQuery {
                tenant: tenant.clone(),
                type_id: type_filter.clone(),
                q: None,
                filters: HashMap::from([("id".to_string(), object_id.to_string())]),
                page: Page {
                    size: 1,
                    token: None,
                },
            },
            ReadConsistency::Eventual,
        )
        .await
        .map_err(|error| format!("search backend lookup failed: {error}"))?;

    if let Some(object) = hits
        .items
        .iter()
        .find_map(|hit| search_hit_to_object_instance(hit, claims.org_id))
    {
        return Ok(Some(object));
    }

    let stored = state
        .stores
        .objects
        .get(
            &tenant,
            &ObjectId(object_id.to_string()),
            ReadConsistency::Eventual,
        )
        .await
        .map_err(|error| format!("object store lookup failed: {error}"))?;

    Ok(stored.and_then(|object| object_store_to_object_instance(object, claims.org_id)))
}

pub async fn list_accessible_objects_by_type(
    state: &AppState,
    claims: &Claims,
    object_type_id: Uuid,
    limit: u32,
) -> Result<Vec<Value>, String> {
    let tenant = tenant_from_claims(claims);
    let hits = state
        .stores
        .search
        .search(
            SearchQuery {
                tenant,
                type_id: Some(TypeId(object_type_id.to_string())),
                q: None,
                filters: HashMap::new(),
                page: Page {
                    size: limit.max(1),
                    token: None,
                },
            },
            ReadConsistency::Eventual,
        )
        .await
        .map_err(|error| format!("search backend type listing failed: {error}"))?;

    Ok(hits
        .items
        .iter()
        .filter_map(|hit| search_hit_to_object_instance(hit, claims.org_id))
        .filter(|object| ensure_object_access(claims, object).is_ok())
        .map(object_to_json)
        .collect())
}

fn parse_uuid(value: Option<&Value>) -> Option<Uuid> {
    value
        .and_then(Value::as_str)
        .and_then(|value| Uuid::parse_str(value).ok())
}

fn parse_datetime(value: Option<&Value>) -> Option<DateTime<Utc>> {
    match value {
        Some(Value::String(value)) => DateTime::parse_from_rfc3339(value)
            .ok()
            .map(|value| value.with_timezone(&Utc)),
        Some(Value::Number(value)) => value
            .as_i64()
            .and_then(|value| Utc.timestamp_millis_opt(value).single()),
        _ => None,
    }
}
