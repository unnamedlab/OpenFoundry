//! PostgreSQL-backed repository boundary for residual ontology definitions.
//!
//! S1 keeps declarative ontology metadata in `pg-schemas` while runtime
//! objects, links, action logs and materialized read models move to Cassandra
//! and search-backed stores. HTTP handlers should not embed `sqlx::*` calls
//! directly; any remaining PG interaction is routed through this module or a
//! typed repository built on top of it.

use sqlx::{
    FromRow, PgPool, Postgres, QueryBuilder,
    postgres::{PgArguments, PgRow},
    query::{Query, QueryAs, QueryScalar},
};
use storage_abstraction::repositories::{
    DefinitionId, DefinitionKind, DefinitionQuery, DefinitionRecord, DefinitionStore, Page,
    PagedResult, PutOutcome, ReadConsistency, RepoError, RepoResult,
};

use async_trait::async_trait;

pub fn typed<'q, O>(sql: &'q str) -> QueryAs<'q, Postgres, O, PgArguments>
where
    O: for<'r> FromRow<'r, PgRow> + Send + Unpin,
{
    sqlx::query_as::<_, O>(sql)
}

pub fn scalar<'q, O>(sql: &'q str) -> QueryScalar<'q, Postgres, O, PgArguments>
where
    (O,): for<'r> FromRow<'r, PgRow>,
    O: Send + Unpin,
{
    sqlx::query_scalar::<_, O>(sql)
}

pub fn raw(sql: &str) -> Query<'_, Postgres, PgArguments> {
    sqlx::query(sql)
}

#[derive(Debug, Clone, Copy)]
struct DefinitionTable {
    table: &'static str,
    owner_col: Option<&'static str>,
    parent_col: Option<&'static str>,
    search_cols: &'static [&'static str],
    filter_cols: &'static [&'static str],
}

fn definition_table(kind: &DefinitionKind) -> Option<DefinitionTable> {
    match kind.0.as_str() {
        "object_type" | "object_types" => Some(DefinitionTable {
            table: "object_types",
            owner_col: Some("owner_id"),
            parent_col: None,
            search_cols: &["name", "display_name"],
            filter_cols: &["name", "owner_id"],
        }),
        "property" | "properties" => Some(DefinitionTable {
            table: "properties",
            owner_col: None,
            parent_col: Some("object_type_id"),
            search_cols: &["name", "display_name", "property_type"],
            filter_cols: &["object_type_id", "name", "property_type"],
        }),
        "shared_property_type" | "shared_property_types" => Some(DefinitionTable {
            table: "shared_property_types",
            owner_col: Some("owner_id"),
            parent_col: None,
            search_cols: &["name", "display_name", "property_type"],
            filter_cols: &["name", "property_type", "owner_id"],
        }),
        "interface" | "interfaces" => Some(DefinitionTable {
            table: "ontology_interfaces",
            owner_col: Some("owner_id"),
            parent_col: None,
            search_cols: &["name", "display_name"],
            filter_cols: &["name", "owner_id"],
        }),
        "interface_property" | "interface_properties" => Some(DefinitionTable {
            table: "interface_properties",
            owner_col: None,
            parent_col: Some("interface_id"),
            search_cols: &["name", "display_name", "property_type"],
            filter_cols: &["interface_id", "name", "property_type"],
        }),
        "link_type" | "link_types" => Some(DefinitionTable {
            table: "link_types",
            owner_col: Some("owner_id"),
            parent_col: None,
            search_cols: &["name", "display_name", "cardinality"],
            filter_cols: &[
                "source_type_id",
                "target_type_id",
                "name",
                "cardinality",
                "owner_id",
            ],
        }),
        "action_type" | "action_types" => Some(DefinitionTable {
            table: "action_types",
            owner_col: Some("owner_id"),
            parent_col: Some("object_type_id"),
            search_cols: &["name", "display_name", "operation_kind"],
            filter_cols: &["object_type_id", "name", "operation_kind", "owner_id"],
        }),
        "rule" | "rules" => Some(DefinitionTable {
            table: "ontology_rules",
            owner_col: Some("owner_id"),
            parent_col: Some("object_type_id"),
            search_cols: &["name", "display_name", "evaluation_mode"],
            filter_cols: &["object_type_id", "name", "evaluation_mode", "owner_id"],
        }),
        "function_package" | "function_packages" => Some(DefinitionTable {
            table: "ontology_function_packages",
            owner_col: Some("owner_id"),
            parent_col: None,
            search_cols: &["name", "display_name", "runtime"],
            filter_cols: &["name", "runtime", "owner_id"],
        }),
        "object_set" | "object_sets" => Some(DefinitionTable {
            table: "ontology_object_sets",
            owner_col: Some("owner_id"),
            parent_col: Some("base_object_type_id"),
            search_cols: &["name", "description"],
            filter_cols: &["base_object_type_id", "name", "owner_id"],
        }),
        "quiver_visual_function" | "quiver_visual_functions" => Some(DefinitionTable {
            table: "ontology_quiver_visual_functions",
            owner_col: Some("owner_id"),
            parent_col: Some("primary_type_id"),
            search_cols: &["name", "description", "chart_kind"],
            filter_cols: &[
                "primary_type_id",
                "secondary_type_id",
                "name",
                "chart_kind",
                "owner_id",
            ],
        }),
        "project" | "projects" => Some(DefinitionTable {
            table: "ontology_projects",
            owner_col: Some("owner_id"),
            parent_col: None,
            search_cols: &["slug", "display_name", "workspace_slug"],
            filter_cols: &["slug", "workspace_slug", "owner_id"],
        }),
        "funnel_source" | "funnel_sources" => Some(DefinitionTable {
            table: "ontology_funnel_sources",
            owner_col: Some("owner_id"),
            parent_col: None,
            search_cols: &["name", "description"],
            filter_cols: &["name", "owner_id"],
        }),
        _ => None,
    }
}

fn unsupported_kind(kind: &DefinitionKind) -> RepoError {
    RepoError::InvalidArgument(format!("unsupported ontology definition kind '{}'", kind.0))
}

fn payload_string(payload: &serde_json::Value, field: &str) -> RepoResult<String> {
    payload
        .get(field)
        .and_then(serde_json::Value::as_str)
        .map(ToOwned::to_owned)
        .ok_or_else(|| RepoError::InvalidArgument(format!("definition payload missing {field}")))
}

fn payload_uuid(payload: &serde_json::Value, field: &str) -> RepoResult<uuid::Uuid> {
    let raw = payload_string(payload, field)?;
    uuid::Uuid::parse_str(&raw)
        .map_err(|error| RepoError::InvalidArgument(format!("{field} is not a UUID: {error}")))
}

fn payload_json(payload: &serde_json::Value, field: &str) -> serde_json::Value {
    payload
        .get(field)
        .cloned()
        .unwrap_or_else(|| serde_json::Value::Array(Vec::new()))
}

fn payload_bool(payload: &serde_json::Value, field: &str) -> bool {
    payload
        .get(field)
        .and_then(serde_json::Value::as_bool)
        .unwrap_or_default()
}

async fn put_action_type_definition(
    pool: &PgPool,
    record: DefinitionRecord,
) -> RepoResult<PutOutcome> {
    let payload = record.payload;
    let id = payload_uuid(&payload, "id")?;
    let name = payload_string(&payload, "name")?;
    let display_name = payload_string(&payload, "display_name")?;
    let description = payload
        .get("description")
        .and_then(serde_json::Value::as_str)
        .unwrap_or_default()
        .to_string();
    let object_type_id = payload_uuid(&payload, "object_type_id")?;
    let operation_kind = payload_string(&payload, "operation_kind")?;
    let input_schema = sqlx::types::Json(payload_json(&payload, "input_schema"));
    let form_schema = sqlx::types::Json(
        payload
            .get("form_schema")
            .cloned()
            .unwrap_or_else(|| serde_json::json!({})),
    );
    let config = sqlx::types::Json(
        payload
            .get("config")
            .cloned()
            .unwrap_or(serde_json::Value::Null),
    );
    let confirmation_required = payload_bool(&payload, "confirmation_required");
    let permission_key = payload
        .get("permission_key")
        .and_then(serde_json::Value::as_str)
        .map(ToOwned::to_owned);
    let authorization_policy = sqlx::types::Json(
        payload
            .get("authorization_policy")
            .cloned()
            .unwrap_or_else(|| serde_json::json!({})),
    );
    let owner_id = payload_uuid(&payload, "owner_id")?;

    let inserted = sqlx::query_scalar::<_, bool>(
        r#"INSERT INTO action_types (
               id, name, display_name, description, object_type_id, operation_kind,
               input_schema, form_schema, config, confirmation_required, permission_key,
               authorization_policy, owner_id
           ) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9::jsonb, $10, $11, $12::jsonb, $13)
           ON CONFLICT (id) DO UPDATE SET
               display_name = EXCLUDED.display_name,
               description = EXCLUDED.description,
               operation_kind = EXCLUDED.operation_kind,
               input_schema = EXCLUDED.input_schema,
               form_schema = EXCLUDED.form_schema,
               config = EXCLUDED.config,
               confirmation_required = EXCLUDED.confirmation_required,
               permission_key = EXCLUDED.permission_key,
               authorization_policy = EXCLUDED.authorization_policy,
               updated_at = now()
           RETURNING (xmax = 0) AS inserted"#,
    )
    .bind(id)
    .bind(name)
    .bind(display_name)
    .bind(description)
    .bind(object_type_id)
    .bind(operation_kind)
    .bind(input_schema)
    .bind(form_schema)
    .bind(config)
    .bind(confirmation_required)
    .bind(permission_key)
    .bind(authorization_policy)
    .bind(owner_id)
    .fetch_one(pool)
    .await
    .map_err(|error| RepoError::Backend(error.to_string()))?;

    if inserted {
        Ok(PutOutcome::Inserted)
    } else {
        Ok(PutOutcome::Updated {
            previous_version: 0,
            new_version: 0,
        })
    }
}

async fn put_object_set_definition(
    pool: &PgPool,
    record: DefinitionRecord,
) -> RepoResult<PutOutcome> {
    let payload = record.payload;
    let id = payload_uuid(&payload, "id")?;
    let name = payload_string(&payload, "name")?;
    let description = payload
        .get("description")
        .and_then(serde_json::Value::as_str)
        .unwrap_or_default()
        .to_string();
    let base_object_type_id = payload_uuid(&payload, "base_object_type_id")?;
    let filters = sqlx::types::Json(payload_json(&payload, "filters"));
    let traversals = sqlx::types::Json(payload_json(&payload, "traversals"));
    let join_config = payload
        .get("join")
        .or_else(|| payload.get("join_config"))
        .cloned()
        .filter(|value| !value.is_null())
        .map(sqlx::types::Json);
    let projections = sqlx::types::Json(payload_json(&payload, "projections"));
    let what_if_label = payload
        .get("what_if_label")
        .and_then(serde_json::Value::as_str)
        .map(ToOwned::to_owned);
    let policy = sqlx::types::Json(
        payload
            .get("policy")
            .cloned()
            .unwrap_or_else(|| serde_json::json!({})),
    );
    let owner_id = payload_uuid(&payload, "owner_id")?;

    let inserted = sqlx::query_scalar::<_, bool>(
        r#"INSERT INTO ontology_object_sets (
               id, name, description, base_object_type_id, filters, traversals, join_config,
               projections, what_if_label, policy, owner_id
           ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
           ON CONFLICT (id) DO UPDATE SET
               name = EXCLUDED.name,
               description = EXCLUDED.description,
               base_object_type_id = EXCLUDED.base_object_type_id,
               filters = EXCLUDED.filters,
               traversals = EXCLUDED.traversals,
               join_config = EXCLUDED.join_config,
               projections = EXCLUDED.projections,
               what_if_label = EXCLUDED.what_if_label,
               policy = EXCLUDED.policy,
               updated_at = now()
           RETURNING (xmax = 0) AS inserted"#,
    )
    .bind(id)
    .bind(name)
    .bind(description)
    .bind(base_object_type_id)
    .bind(filters)
    .bind(traversals)
    .bind(join_config)
    .bind(projections)
    .bind(what_if_label)
    .bind(policy)
    .bind(owner_id)
    .fetch_one(pool)
    .await
    .map_err(|error| RepoError::Backend(error.to_string()))?;

    if inserted {
        Ok(PutOutcome::Inserted)
    } else {
        Ok(PutOutcome::Updated {
            previous_version: 0,
            new_version: 0,
        })
    }
}

fn id_from_payload(payload: &serde_json::Value) -> RepoResult<DefinitionId> {
    payload
        .get("id")
        .and_then(serde_json::Value::as_str)
        .map(|id| DefinitionId(id.to_string()))
        .ok_or_else(|| RepoError::Backend("definition row did not project an id".to_string()))
}

fn optional_string(payload: &serde_json::Value, field: &str) -> Option<String> {
    payload
        .get(field)
        .and_then(serde_json::Value::as_str)
        .map(ToOwned::to_owned)
}

fn record_from_payload(
    kind: &DefinitionKind,
    table: DefinitionTable,
    payload: serde_json::Value,
) -> RepoResult<DefinitionRecord> {
    let id = id_from_payload(&payload)?;
    let owner_id = table
        .owner_col
        .and_then(|column| optional_string(&payload, column));
    let parent_id = table
        .parent_col
        .and_then(|column| optional_string(&payload, column))
        .map(DefinitionId);

    Ok(DefinitionRecord {
        kind: kind.clone(),
        id,
        tenant: None,
        owner_id,
        parent_id,
        version: payload
            .get("version")
            .and_then(serde_json::Value::as_u64)
            .or_else(|| payload.get("revision").and_then(serde_json::Value::as_u64)),
        payload,
        created_at_ms: None,
        updated_at_ms: None,
    })
}

fn push_definition_filters<'q>(
    builder: &mut QueryBuilder<'q, Postgres>,
    table: DefinitionTable,
    query: &'q DefinitionQuery,
) {
    if let (Some(owner_col), Some(owner_id)) = (table.owner_col, query.owner_id.as_ref()) {
        builder.push(" AND ");
        builder.push(owner_col);
        builder.push("::text = ");
        builder.push_bind(owner_id);
    }

    if let (Some(parent_col), Some(parent_id)) = (table.parent_col, query.parent_id.as_ref()) {
        builder.push(" AND ");
        builder.push(parent_col);
        builder.push("::text = ");
        builder.push_bind(&parent_id.0);
    }

    for (column, value) in &query.filters {
        if table.filter_cols.iter().any(|allowed| allowed == column) {
            builder.push(" AND ");
            builder.push(column.as_str());
            builder.push("::text = ");
            builder.push_bind(value);
        }
    }

    if let Some(search) = query
        .search
        .as_ref()
        .map(|s| s.trim())
        .filter(|s| !s.is_empty())
    {
        builder.push(" AND (");
        for (idx, column) in table.search_cols.iter().enumerate() {
            if idx > 0 {
                builder.push(" OR ");
            }
            builder.push(*column);
            builder.push(" ILIKE ");
            builder.push_bind(format!("%{search}%"));
        }
        builder.push(")");
    }
}

fn page_offset(page: &Page) -> RepoResult<u32> {
    match page.token.as_deref() {
        Some(token) => token.parse::<u32>().map_err(|_| {
            RepoError::InvalidArgument("definition page token is invalid".to_string())
        }),
        None => Ok(0),
    }
}

/// PostgreSQL-backed [`DefinitionStore`] for residual declarative ontology
/// metadata. It is deliberately table-map based: callers pass a logical
/// `DefinitionKind`, and only known kinds are translated to SQL.
pub struct PostgresDefinitionStore {
    pool: PgPool,
}

impl PostgresDefinitionStore {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl DefinitionStore for PostgresDefinitionStore {
    async fn get(
        &self,
        kind: &DefinitionKind,
        id: &DefinitionId,
        _consistency: ReadConsistency,
    ) -> RepoResult<Option<DefinitionRecord>> {
        let table = definition_table(kind).ok_or_else(|| unsupported_kind(kind))?;
        let sql = format!(
            "SELECT to_jsonb(row) AS payload FROM (SELECT * FROM {} WHERE id::text = $1) row",
            table.table
        );
        let payload = sqlx::query_scalar::<_, serde_json::Value>(&sql)
            .bind(&id.0)
            .fetch_optional(&self.pool)
            .await
            .map_err(|error| RepoError::Backend(error.to_string()))?;

        payload
            .map(|payload| record_from_payload(kind, table, payload))
            .transpose()
    }

    async fn list(
        &self,
        query: DefinitionQuery,
        _consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<DefinitionRecord>> {
        let table = definition_table(&query.kind).ok_or_else(|| unsupported_kind(&query.kind))?;
        let offset = page_offset(&query.page)?;
        let limit = query.page.size.max(1);

        let mut builder =
            QueryBuilder::<Postgres>::new("SELECT to_jsonb(row) AS payload FROM (SELECT * FROM ");
        builder.push(table.table);
        builder.push(" WHERE TRUE");
        push_definition_filters(&mut builder, table, &query);
        builder.push(" ORDER BY COALESCE(updated_at, created_at) DESC, id ASC LIMIT ");
        builder.push_bind(i64::from(limit + 1));
        builder.push(" OFFSET ");
        builder.push_bind(i64::from(offset));
        builder.push(") row");

        let payloads = builder
            .build_query_scalar::<serde_json::Value>()
            .fetch_all(&self.pool)
            .await
            .map_err(|error| RepoError::Backend(error.to_string()))?;

        let has_more = payloads.len() > limit as usize;
        let items = payloads
            .into_iter()
            .take(limit as usize)
            .map(|payload| record_from_payload(&query.kind, table, payload))
            .collect::<RepoResult<Vec<_>>>()?;

        Ok(PagedResult {
            items,
            next_token: has_more.then(|| (offset + limit).to_string()),
        })
    }

    async fn put(
        &self,
        record: DefinitionRecord,
        _expected_version: Option<u64>,
    ) -> RepoResult<PutOutcome> {
        if matches!(record.kind.0.as_str(), "action_type" | "action_types") {
            return put_action_type_definition(&self.pool, record).await;
        }
        if matches!(record.kind.0.as_str(), "object_set" | "object_sets") {
            return put_object_set_definition(&self.pool, record).await;
        }
        Err(RepoError::Backend(
            "PostgresDefinitionStore::put is intentionally not generic; use the typed definition repository for the owning model".to_string(),
        ))
    }

    async fn delete(&self, kind: &DefinitionKind, id: &DefinitionId) -> RepoResult<bool> {
        let table = definition_table(kind).ok_or_else(|| unsupported_kind(kind))?;
        let sql = format!("DELETE FROM {} WHERE id::text = $1", table.table);
        let result = sqlx::query(&sql)
            .bind(&id.0)
            .execute(&self.pool)
            .await
            .map_err(|error| RepoError::Backend(error.to_string()))?;
        Ok(result.rows_affected() > 0)
    }

    async fn count(
        &self,
        query: DefinitionQuery,
        _consistency: ReadConsistency,
    ) -> RepoResult<u64> {
        let table = definition_table(&query.kind).ok_or_else(|| unsupported_kind(&query.kind))?;
        let mut builder = QueryBuilder::<Postgres>::new("SELECT COUNT(*) FROM ");
        builder.push(table.table);
        builder.push(" WHERE TRUE");
        push_definition_filters(&mut builder, table, &query);

        let count = builder
            .build_query_scalar::<i64>()
            .fetch_one(&self.pool)
            .await
            .map_err(|error| RepoError::Backend(error.to_string()))?;
        u64::try_from(count).map_err(|_| {
            RepoError::Backend(format!("negative count returned for {}", query.kind.0))
        })
    }
}
