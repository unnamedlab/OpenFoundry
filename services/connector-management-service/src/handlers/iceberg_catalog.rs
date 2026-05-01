//! Minimal Iceberg REST Catalog surface for Foundry-style virtual tables.
//!
//! Implements just enough of the [Apache Iceberg REST Catalog OpenAPI spec ↗](
//!   https://github.com/apache/iceberg/blob/main/open-api/rest-catalog-open-api.yaml)
//! to allow PyIceberg / Trino / Spark / Snowflake (Polaris) clients to:
//!
//!   1. discover the namespaces that Foundry exposes (one per `Connection`
//!      that has at least one zero-copy registration);
//!   2. list the tables in a namespace (one per `connection_registrations`
//!      row whose `metadata.discovery.supports_zero_copy = true`);
//!   3. load a table's metadata pointer.
//!
//! For sources whose underlying format is **already Iceberg or Delta** (S3
//! Iceberg, ADLS Delta, BigQuery Iceberg, Databricks Unity Catalog managed
//! Iceberg…), the `metadata-location` returned by `LoadTable` is the
//! upstream pointer captured at registration time — clients then read the
//! Parquet/Avro data files directly from object storage, exactly the
//! "credential vending" pattern Foundry documents in *Authenticating Iceberg
//! clients*. No bytes flow through Foundry.
//!
//! For sources that are *not* natively Iceberg (Postgres, MySQL, REST APIs,
//! Salesforce, …) we synthesise a Foundry-managed metadata document that
//! advertises a `foundry-vended` location pointing back at our own
//! [`crate::handlers::registrations::query_registration`] endpoint. Engines
//! that follow the spec can still discover the table and Foundry mediates
//! the data path, fulfilling the Foundry guarantee that "the source system
//! is abstracted away".
//!
//! Auth is taken from the standard `Authorization: Bearer <token>` header
//! (decoded by `optional_auth_layer`). Per the Iceberg spec, anonymous
//! requests are rejected with 401.

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde::Serialize;
use serde_json::{Value, json};
use std::collections::BTreeMap;

use crate::{
    AppState,
    models::{connection::Connection, registration::ConnectionRegistration},
};

/// `GET /iceberg/v1/config` — returns catalog defaults & overrides.
pub async fn get_config(AuthUser(_): AuthUser) -> impl IntoResponse {
    Json(json!({
        "defaults": { "warehouse": "openfoundry" },
        "overrides": {},
    }))
}

/// `GET /iceberg/v1/namespaces` — list all namespaces (one per connection
/// that owns at least one zero-copy-capable registration).
pub async fn list_namespaces(
    AuthUser(_): AuthUser,
    State(state): State<AppState>,
) -> impl IntoResponse {
    let rows = sqlx::query_as::<_, (String,)>(
        "SELECT DISTINCT c.name
           FROM connections c
           JOIN connection_registrations r ON r.connection_id = c.id
          WHERE COALESCE((r.metadata->>'supports_zero_copy')::bool, false) = true
          ORDER BY c.name",
    )
    .fetch_all(&state.db)
    .await;
    match rows {
        Ok(items) => Json(json!({
            "namespaces": items
                .into_iter()
                .map(|(name,)| vec![namespace_segment(&name)])
                .collect::<Vec<_>>(),
        }))
        .into_response(),
        Err(error) => {
            tracing::error!("iceberg list namespaces failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

/// `GET /iceberg/v1/namespaces/{namespace}` — namespace metadata.
pub async fn get_namespace(
    AuthUser(_): AuthUser,
    State(state): State<AppState>,
    Path(namespace): Path<String>,
) -> impl IntoResponse {
    let connection = match resolve_connection(&state, &namespace).await {
        Ok(Some(c)) => c,
        Ok(None) => return iceberg_not_found("namespace", &namespace),
        Err(response) => return response,
    };
    Json(json!({
        "namespace": [namespace_segment(&connection.name)],
        "properties": {
            "connection_id": connection.id.to_string(),
            "connector_type": connection.connector_type,
            "owner": connection.owner_id.to_string(),
        },
    }))
    .into_response()
}

/// `GET /iceberg/v1/namespaces/{namespace}/tables` — list tables.
pub async fn list_tables(
    AuthUser(_): AuthUser,
    State(state): State<AppState>,
    Path(namespace): Path<String>,
) -> impl IntoResponse {
    let connection = match resolve_connection(&state, &namespace).await {
        Ok(Some(c)) => c,
        Ok(None) => return iceberg_not_found("namespace", &namespace),
        Err(response) => return response,
    };
    let regs = match fetch_zero_copy_registrations(&state, connection.id).await {
        Ok(items) => items,
        Err(response) => return response,
    };
    Json(json!({
        "identifiers": regs
            .into_iter()
            .map(|r| json!({
                "namespace": [namespace_segment(&connection.name)],
                "name": table_segment(&r.selector),
            }))
            .collect::<Vec<_>>(),
    }))
    .into_response()
}

/// `GET /iceberg/v1/namespaces/{namespace}/tables/{table}` — load table.
///
/// Returns either the upstream Iceberg metadata pointer (when the
/// registration captured one) or a synthetic Foundry-vended document.
pub async fn load_table(
    AuthUser(_): AuthUser,
    State(state): State<AppState>,
    Path((namespace, table)): Path<(String, String)>,
) -> impl IntoResponse {
    let connection = match resolve_connection(&state, &namespace).await {
        Ok(Some(c)) => c,
        Ok(None) => return iceberg_not_found("namespace", &namespace),
        Err(response) => return response,
    };
    let registration = match resolve_registration(&state, connection.id, &table).await {
        Ok(Some(r)) => r,
        Ok(None) => return iceberg_not_found("table", &table),
        Err(response) => return response,
    };
    Json(load_table_response(&connection, &registration).await).into_response()
}

// ─────────────────────── helpers ───────────────────────

/// `Connection` identifier exposed to Iceberg clients. Foundry namespaces
/// support multi-segment identifiers — for now we map each connection to a
/// single segment (its `name`). Engines that paste back the segment we sent
/// will receive the same connection on lookups.
fn namespace_segment(connection_name: &str) -> String {
    sanitize(connection_name)
}

/// Iceberg table identifier exposed to clients. Selectors often look like
/// `public.orders`; clients pass them back URL-encoded. Foundry treats the
/// whole selector as one identifier so we keep the dot.
fn table_segment(selector: &str) -> String {
    selector.to_string()
}

fn sanitize(value: &str) -> String {
    value
        .chars()
        .map(|c| {
            if c.is_ascii_alphanumeric() || c == '_' || c == '-' {
                c
            } else {
                '_'
            }
        })
        .collect()
}

async fn resolve_connection(
    state: &AppState,
    namespace: &str,
) -> Result<Option<Connection>, axum::response::Response> {
    // Iceberg clients send namespaces as plain strings or `unit.separator`
    // delimited segments. We only use the first segment.
    let head = namespace
        .split('\u{1f}')
        .next()
        .unwrap_or(namespace)
        .to_string();
    sqlx::query_as::<_, Connection>(
        "SELECT * FROM connections WHERE name = $1
            OR regexp_replace(name, '[^A-Za-z0-9_-]', '_', 'g') = $1
         LIMIT 1",
    )
    .bind(&head)
    .fetch_optional(&state.db)
    .await
    .map_err(|error| {
        tracing::error!("iceberg connection lookup failed: {error}");
        StatusCode::INTERNAL_SERVER_ERROR.into_response()
    })
}

async fn resolve_registration(
    state: &AppState,
    connection_id: uuid::Uuid,
    table: &str,
) -> Result<Option<ConnectionRegistration>, axum::response::Response> {
    sqlx::query_as::<_, ConnectionRegistration>(
        "SELECT * FROM connection_registrations
          WHERE connection_id = $1 AND selector = $2 LIMIT 1",
    )
    .bind(connection_id)
    .bind(table)
    .fetch_optional(&state.db)
    .await
    .map_err(|error| {
        tracing::error!("iceberg registration lookup failed: {error}");
        StatusCode::INTERNAL_SERVER_ERROR.into_response()
    })
}

async fn fetch_zero_copy_registrations(
    state: &AppState,
    connection_id: uuid::Uuid,
) -> Result<Vec<ConnectionRegistration>, axum::response::Response> {
    sqlx::query_as::<_, ConnectionRegistration>(
        "SELECT * FROM connection_registrations
          WHERE connection_id = $1
            AND COALESCE((metadata->>'supports_zero_copy')::bool, false) = true
          ORDER BY selector",
    )
    .bind(connection_id)
    .fetch_all(&state.db)
    .await
    .map_err(|error| {
        tracing::error!("iceberg list registrations failed: {error}");
        StatusCode::INTERNAL_SERVER_ERROR.into_response()
    })
}

fn iceberg_not_found(kind: &str, value: &str) -> axum::response::Response {
    (
        StatusCode::NOT_FOUND,
        Json(json!({
            "error": {
                "message": format!("{kind} '{value}' not found"),
                "type": "NoSuchNamespaceException",
                "code": 404,
            }
        })),
    )
        .into_response()
}

/// Builds a `LoadTableResponse` per Iceberg REST spec.
///
/// If the registration metadata contains an upstream `metadata_location` (set
/// by S3/ADLS/Databricks discovery for Iceberg-formatted tables), we return
/// that verbatim — clients then fetch the metadata.json and data files
/// straight from the lake. Otherwise we synthesise a Foundry-managed
/// document and point clients back at our `query_registration` endpoint via
/// the `foundry-vended` config key.
///
/// Storage credentials are issued via [`vended_credentials`] following the
/// Iceberg REST credential-vending pattern documented in
/// `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/
/// Workflows/Iceberg tables/Authenticating Iceberg clients.md`. The catalog
/// is the only party that ever sees the underlying source credential — the
/// client receives a short-lived, table-scoped snapshot keyed by
/// `expires-at-ms`.
async fn load_table_response(connection: &Connection, registration: &ConnectionRegistration) -> Value {
    let upstream_metadata = registration
        .metadata
        .pointer("/discovery/upstream/metadata_location")
        .and_then(Value::as_str)
        .or_else(|| {
            registration
                .metadata
                .pointer("/upstream/metadata_location")
                .and_then(Value::as_str)
        });

    let mut config = BTreeMap::<&str, Value>::new();
    config.insert("connection_id", json!(connection.id.to_string()));
    config.insert("registration_id", json!(registration.id.to_string()));
    config.insert("connector_type", json!(connection.connector_type));
    config.insert("source_kind", json!(registration.source_kind));
    if upstream_metadata.is_none() {
        config.insert(
            "foundry-vended",
            json!(format!(
                "/api/v1/data-connection/sources/{}/registrations/{}/query",
                connection.id, registration.id
            )),
        );
    }
    let vended = super::credentials_vending::vend(connection, vended_ttl_secs()).await;
    for (key, value) in vended.entries {
        config.insert(key, value);
    }

    // Compute-pushdown hints. For source systems that are *not* object
    // stores (Snowflake, Databricks, BigQuery), the upstream catalog does
    // not own a `metadata.json` we can forward. Instead, Foundry's virtual
    // table model for these engines sends clients back to the source's
    // native compute via JDBC / Storage API. We expose the connection
    // coordinates here so a compatible client (Trino/Spark with the
    // appropriate connector) can route the query downstream. See
    // `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Core concepts/Virtual tables.md`
    // (matrix § "Compute pushdown").
    for (key, value) in pushdown_config(connection, registration) {
        config.insert(key, value);
    }

    let metadata_location = upstream_metadata
        .map(str::to_string)
        .unwrap_or_else(|| {
            format!(
                "openfoundry://catalog/{}/{}/v0.metadata.json",
                connection.id, registration.id
            )
        });

    json!({
        "metadata-location": metadata_location,
        "metadata": synthetic_table_metadata(connection, registration),
        "config": config,
    })
}

/// Time-to-live for vended credential snapshots. Foundry rotates STS/SAS
/// tokens every ~15 minutes; we mirror that default and let operators
/// override via `OPENFOUNDRY_VENDED_CREDENTIALS_TTL_SECS`.
fn vended_ttl_secs() -> i64 {
    std::env::var("OPENFOUNDRY_VENDED_CREDENTIALS_TTL_SECS")
        .ok()
        .and_then(|raw| raw.parse::<i64>().ok())
        .filter(|value| *value > 0)
        .unwrap_or(900)
}

/// Build per-engine compute-pushdown config keys for Snowflake/Databricks/
/// BigQuery virtual tables. Returns an empty vec for object-store sources
/// (their LoadTable already carries a real `metadata-location`).
fn pushdown_config(
    connection: &Connection,
    registration: &ConnectionRegistration,
) -> Vec<(&'static str, Value)> {
    let cfg = &connection.config;
    let str_field = |k: &str| cfg.get(k).and_then(Value::as_str).map(str::to_string);
    let mut entries: Vec<(&'static str, Value)> = Vec::new();
    match connection.connector_type.as_str() {
        "snowflake" => {
            entries.push(("pushdown.engine", json!("snowflake")));
            if let Some(v) = str_field("account") {
                entries.push(("pushdown.snowflake.account", json!(v)));
            }
            if let Some(v) = str_field("warehouse") {
                entries.push(("pushdown.snowflake.warehouse", json!(v)));
            }
            if let Some(v) = str_field("database") {
                entries.push(("pushdown.snowflake.database", json!(v)));
            }
            if let Some(v) = str_field("schema") {
                entries.push(("pushdown.snowflake.schema", json!(v)));
            }
            entries.push((
                "pushdown.snowflake.fqn",
                json!(qualify(cfg, &registration.selector)),
            ));
        }
        "databricks" => {
            entries.push(("pushdown.engine", json!("databricks")));
            if let Some(v) = str_field("workspace_url") {
                entries.push(("pushdown.databricks.workspace_url", json!(v)));
            }
            if let Some(v) = str_field("http_path") {
                entries.push(("pushdown.databricks.http_path", json!(v)));
            }
            if let Some(v) = str_field("catalog") {
                entries.push(("pushdown.databricks.catalog", json!(v)));
            }
            entries.push((
                "pushdown.databricks.fqn",
                json!(qualify(cfg, &registration.selector)),
            ));
        }
        "bigquery" => {
            entries.push(("pushdown.engine", json!("bigquery")));
            if let Some(v) = str_field("project_id") {
                entries.push(("pushdown.bigquery.project_id", json!(v)));
            }
            if let Some(v) = str_field("dataset") {
                entries.push(("pushdown.bigquery.dataset", json!(v)));
            }
            entries.push((
                "pushdown.bigquery.table",
                json!(qualify(cfg, &registration.selector)),
            ));
        }
        "generic" => {
            if let Some(v) = str_field("catalog_url") {
                entries.push(("pushdown.engine", json!("iceberg-rest")));
                entries.push(("pushdown.iceberg.catalog_url", json!(v)));
            }
        }
        _ => {}
    }
    entries
}

/// Best-effort 3-part name. Selectors that already look fully qualified
/// (`db.schema.table`) are returned verbatim; otherwise we prefix with the
/// connection's default database/schema/catalog when present.
fn qualify(config: &Value, selector: &str) -> String {
    if selector.matches('.').count() >= 2 {
        return selector.to_string();
    }
    let db = config
        .get("database")
        .or_else(|| config.get("catalog"))
        .or_else(|| config.get("project_id"))
        .and_then(Value::as_str);
    let schema = config
        .get("schema")
        .or_else(|| config.get("dataset"))
        .and_then(Value::as_str);
    match (db, schema) {
        (Some(d), Some(s)) if !selector.contains('.') => format!("{d}.{s}.{selector}"),
        (Some(d), _) if !selector.contains('.') => format!("{d}.{selector}"),
        _ => selector.to_string(),
    }
}

/// Minimal Iceberg `TableMetadata`-shaped document. Real Iceberg sources
/// come with their own metadata.json over the wire (we just forward the
/// pointer). For Foundry-mediated sources we emit just enough fields so that
/// PyIceberg/Trino can complete a `load_table()` call without crashing.
fn synthetic_table_metadata(connection: &Connection, registration: &ConnectionRegistration) -> Value {
    json!({
        "format-version": 2,
        "table-uuid": registration.id,
        "location": format!(
            "openfoundry://connector/{}/{}",
            connection.connector_type, registration.selector
        ),
        "last-updated-ms": registration.updated_at.timestamp_millis(),
        "schemas": [{ "schema-id": 0, "type": "struct", "fields": [] }],
        "current-schema-id": 0,
        "partition-specs": [{ "spec-id": 0, "fields": [] }],
        "default-spec-id": 0,
        "snapshots": [],
        "properties": {
            "openfoundry.connection_id": connection.id.to_string(),
            "openfoundry.registration_id": registration.id.to_string(),
            "openfoundry.connector_type": connection.connector_type.clone(),
            "openfoundry.source_kind": registration.source_kind.clone(),
            "openfoundry.last_source_signature":
                registration.last_source_signature.clone().unwrap_or_default(),
        },
    })
}

#[derive(Serialize)]
#[allow(dead_code)]
pub(crate) struct CatalogConfig {
    pub defaults: BTreeMap<String, String>,
    pub overrides: BTreeMap<String, String>,
}
