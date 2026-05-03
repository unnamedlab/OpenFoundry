//! `object-database-service` runtime owner for ontology object storage.
//!
//! The service speaks the shared [`ObjectStore`] contract and owns the
//! Cassandra `ontology_objects` / `ontology_indexes` schema. Postgres is not
//! part of this runtime path; declarative ontology schemas remain in
//! `ontology-definition-service`, and outbox writes are handled by callers that
//! need transactional publication.

use std::{net::SocketAddr, sync::Arc};

use axum::{
    Json, Router,
    extract::{Path, Query, State},
    http::StatusCode,
    response::{IntoResponse, Response},
    routing::get,
};
use cassandra_kernel::{
    ClusterConfig, Migration, SessionBuilder, migrate,
    repos::{CassandraLinkStore, CassandraObjectStore},
};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use storage_abstraction::repositories::{
    Link, LinkStore, LinkTypeId, MarkingId, Object, ObjectId, ObjectStore, OwnerId, Page,
    PutOutcome, ReadConsistency, RepoError, TenantId, TypeId,
};
use tower_http::trace::TraceLayer;
use tracing_subscriber::EnvFilter;

const ONTOLOGY_OBJECTS_KEYSPACE: &str = include_str!("../cql/ontology_objects/000_keyspace.cql");
const ONTOLOGY_INDEXES_KEYSPACE: &str = include_str!("../cql/ontology_indexes/000_keyspace.cql");

const ONTOLOGY_OBJECTS_MIGRATIONS: &[Migration] = &[Migration {
    version: 1,
    name: "ontology_object_tables",
    statements: &[
        include_str!("../cql/ontology_objects/001_objects_by_id.cql"),
        include_str!("../cql/ontology_objects/002_objects_by_type.cql"),
        include_str!("../cql/ontology_objects/003_objects_by_owner.cql"),
        include_str!("../cql/ontology_objects/004_objects_by_marking.cql"),
    ],
}];

const ONTOLOGY_INDEXES_MIGRATIONS: &[Migration] = &[Migration {
    version: 1,
    name: "ontology_link_indexes",
    statements: &[
        include_str!("../cql/ontology_indexes/001_links_outgoing.cql"),
        include_str!("../cql/ontology_indexes/002_links_incoming.cql"),
    ],
}];

#[derive(Debug, Clone, Deserialize)]
struct AppConfig {
    #[serde(default = "default_host")]
    host: String,
    #[serde(default = "default_port")]
    port: u16,
    #[serde(default)]
    cassandra_contact_points: String,
    #[serde(default = "default_local_dc")]
    cassandra_local_dc: String,
}

impl AppConfig {
    fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }
}

#[derive(Clone)]
struct ObjectDatabaseState {
    objects: Arc<dyn ObjectStore>,
    links: Arc<dyn LinkStore>,
    backend: BackendMode,
}

#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
enum BackendMode {
    Cassandra,
    InMemory,
}

#[derive(Debug, Deserialize)]
struct ListQuery {
    #[serde(default = "default_page_size")]
    size: u32,
    token: Option<String>,
    consistency: Option<String>,
}

#[derive(Debug, Deserialize)]
struct WriteObjectRequest {
    type_id: String,
    version: u64,
    payload: Value,
    expected_version: Option<u64>,
    owner: Option<String>,
    #[serde(default)]
    markings: Vec<String>,
    organization_id: Option<String>,
    created_at_ms: Option<i64>,
    updated_at_ms: Option<i64>,
}

#[derive(Debug, Serialize)]
struct WriteObjectResponse {
    outcome: &'static str,
    previous_version: Option<u64>,
    new_version: Option<u64>,
    expected_version: Option<u64>,
    actual_version: Option<u64>,
}

#[derive(Debug, Serialize)]
struct ObjectListResponse {
    items: Vec<Object>,
    next_token: Option<String>,
}

#[derive(Debug, Serialize)]
struct LinkListResponse {
    items: Vec<Link>,
    next_token: Option<String>,
}

#[derive(Debug, Serialize)]
struct StatusResponse {
    service: &'static str,
    ready: bool,
    backend: BackendMode,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| EnvFilter::new("object_database_service=info,tower_http=info")),
        )
        .init();

    let cfg = AppConfig::from_env()?;
    let state = build_state(&cfg).await?;
    let app = build_router(state).layer(TraceLayer::new_for_http());

    let addr: SocketAddr = format!("{}:{}", cfg.host, cfg.port).parse()?;
    tracing::info!(%addr, "starting object-database-service");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}

fn build_router(state: ObjectDatabaseState) -> Router {
    Router::new()
        .route("/health", get(|| async { "ok" }))
        .route("/ready", get(readiness))
        .route("/readiness", get(readiness))
        .route("/status", get(status))
        .nest(
            "/api/v1/object-database",
            Router::new()
                .route("/status", get(status))
                .route(
                    "/objects/{tenant}/{object_id}",
                    get(get_object).put(put_object).delete(delete_object),
                )
                .route(
                    "/objects/{tenant}/by-type/{type_id}",
                    get(list_objects_by_type),
                )
                .route(
                    "/objects/{tenant}/by-owner/{owner_id}",
                    get(list_objects_by_owner),
                )
                .route(
                    "/objects/{tenant}/by-marking/{marking_id}",
                    get(list_objects_by_marking),
                )
                .route(
                    "/links/{tenant}/{link_type}/outgoing/{from}",
                    get(list_outgoing_links),
                )
                .route(
                    "/links/{tenant}/{link_type}/incoming/{to}",
                    get(list_incoming_links),
                ),
        )
        .with_state(state)
}

async fn build_state(cfg: &AppConfig) -> Result<ObjectDatabaseState, Box<dyn std::error::Error>> {
    if cfg.cassandra_contact_points.trim().is_empty() {
        tracing::warn!(
            "CASSANDRA_CONTACT_POINTS not set; using in-memory stores. \
             Production deployments must set Cassandra contact points."
        );
        return Ok(ObjectDatabaseState {
            objects: Arc::new(
                storage_abstraction::repositories::noop::InMemoryObjectStore::default(),
            ),
            links: Arc::new(storage_abstraction::repositories::noop::InMemoryLinkStore::default()),
            backend: BackendMode::InMemory,
        });
    }

    let cluster = ClusterConfig {
        contact_points: cfg.cassandra_points(),
        local_datacenter: cfg.cassandra_local_dc.clone(),
        ..ClusterConfig::dev_local()
    };
    let session = Arc::new(SessionBuilder::new(cluster).build().await?);
    session.query(ONTOLOGY_OBJECTS_KEYSPACE, &[]).await?;
    session.query(ONTOLOGY_INDEXES_KEYSPACE, &[]).await?;
    migrate::apply(
        session.as_ref(),
        "ontology_objects",
        ONTOLOGY_OBJECTS_MIGRATIONS,
    )
    .await?;
    migrate::apply(
        session.as_ref(),
        "ontology_indexes",
        ONTOLOGY_INDEXES_MIGRATIONS,
    )
    .await?;

    let object_store = CassandraObjectStore::new(session.clone());
    object_store.warm_up().await?;
    let link_store = CassandraLinkStore::new(session);
    link_store.warm_up().await?;

    Ok(ObjectDatabaseState {
        objects: Arc::new(object_store),
        links: Arc::new(link_store),
        backend: BackendMode::Cassandra,
    })
}

impl AppConfig {
    fn cassandra_points(&self) -> Vec<String> {
        self.cassandra_contact_points
            .split(',')
            .map(str::trim)
            .filter(|point| !point.is_empty())
            .map(ToOwned::to_owned)
            .collect()
    }
}

async fn readiness(State(state): State<ObjectDatabaseState>) -> Response {
    if matches!(
        state.backend,
        BackendMode::Cassandra | BackendMode::InMemory
    ) {
        Json(StatusResponse {
            service: "object-database-service",
            ready: true,
            backend: state.backend,
        })
        .into_response()
    } else {
        StatusCode::SERVICE_UNAVAILABLE.into_response()
    }
}

async fn status(State(state): State<ObjectDatabaseState>) -> Response {
    Json(StatusResponse {
        service: "object-database-service",
        ready: true,
        backend: state.backend,
    })
    .into_response()
}

async fn get_object(
    State(state): State<ObjectDatabaseState>,
    Path((tenant, object_id)): Path<(String, String)>,
    Query(query): Query<ListQuery>,
) -> Response {
    match state
        .objects
        .get(
            &TenantId(tenant),
            &ObjectId(object_id),
            parse_consistency(query.consistency.as_deref()),
        )
        .await
    {
        Ok(Some(object)) => Json(object).into_response(),
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => repo_error_response(error),
    }
}

async fn list_objects_by_type(
    State(state): State<ObjectDatabaseState>,
    Path((tenant, type_id)): Path<(String, String)>,
    Query(query): Query<ListQuery>,
) -> Response {
    match state
        .objects
        .list_by_type(
            &TenantId(tenant),
            &TypeId(type_id),
            Page {
                size: query.size.clamp(1, 5_000),
                token: query.token,
            },
            parse_consistency(query.consistency.as_deref()),
        )
        .await
    {
        Ok(page) => Json(ObjectListResponse {
            items: page.items,
            next_token: page.next_token,
        })
        .into_response(),
        Err(error) => repo_error_response(error),
    }
}

async fn list_objects_by_owner(
    State(state): State<ObjectDatabaseState>,
    Path((tenant, owner_id)): Path<(String, String)>,
    Query(query): Query<ListQuery>,
) -> Response {
    match state
        .objects
        .list_by_owner(
            &TenantId(tenant),
            &OwnerId(owner_id),
            page_from_query(&query),
            parse_consistency(query.consistency.as_deref()),
        )
        .await
    {
        Ok(page) => Json(ObjectListResponse {
            items: page.items,
            next_token: page.next_token,
        })
        .into_response(),
        Err(error) => repo_error_response(error),
    }
}

async fn list_objects_by_marking(
    State(state): State<ObjectDatabaseState>,
    Path((tenant, marking_id)): Path<(String, String)>,
    Query(query): Query<ListQuery>,
) -> Response {
    match state
        .objects
        .list_by_marking(
            &TenantId(tenant),
            &MarkingId(marking_id),
            page_from_query(&query),
            parse_consistency(query.consistency.as_deref()),
        )
        .await
    {
        Ok(page) => Json(ObjectListResponse {
            items: page.items,
            next_token: page.next_token,
        })
        .into_response(),
        Err(error) => repo_error_response(error),
    }
}

async fn list_outgoing_links(
    State(state): State<ObjectDatabaseState>,
    Path((tenant, link_type, from)): Path<(String, String, String)>,
    Query(query): Query<ListQuery>,
) -> Response {
    match state
        .links
        .list_outgoing(
            &TenantId(tenant),
            &LinkTypeId(link_type),
            &ObjectId(from),
            page_from_query(&query),
            parse_consistency(query.consistency.as_deref()),
        )
        .await
    {
        Ok(page) => Json(LinkListResponse {
            items: page.items,
            next_token: page.next_token,
        })
        .into_response(),
        Err(error) => repo_error_response(error),
    }
}

async fn list_incoming_links(
    State(state): State<ObjectDatabaseState>,
    Path((tenant, link_type, to)): Path<(String, String, String)>,
    Query(query): Query<ListQuery>,
) -> Response {
    match state
        .links
        .list_incoming(
            &TenantId(tenant),
            &LinkTypeId(link_type),
            &ObjectId(to),
            page_from_query(&query),
            parse_consistency(query.consistency.as_deref()),
        )
        .await
    {
        Ok(page) => Json(LinkListResponse {
            items: page.items,
            next_token: page.next_token,
        })
        .into_response(),
        Err(error) => repo_error_response(error),
    }
}

async fn put_object(
    State(state): State<ObjectDatabaseState>,
    Path((tenant, object_id)): Path<(String, String)>,
    Json(body): Json<WriteObjectRequest>,
) -> Response {
    let now_ms = chrono::Utc::now().timestamp_millis();
    let object = Object {
        tenant: TenantId(tenant),
        id: ObjectId(object_id),
        type_id: TypeId(body.type_id),
        version: body.version,
        payload: body.payload,
        organization_id: body.organization_id,
        created_at_ms: body.created_at_ms.or(Some(now_ms)),
        updated_at_ms: body.updated_at_ms.unwrap_or(now_ms),
        owner: body.owner.map(OwnerId),
        markings: body.markings.into_iter().map(MarkingId).collect(),
    };

    match state.objects.put(object, body.expected_version).await {
        Ok(outcome) => Json(write_response(outcome)).into_response(),
        Err(error) => repo_error_response(error),
    }
}

async fn delete_object(
    State(state): State<ObjectDatabaseState>,
    Path((tenant, object_id)): Path<(String, String)>,
) -> Response {
    match state
        .objects
        .delete(&TenantId(tenant), &ObjectId(object_id))
        .await
    {
        Ok(true) => StatusCode::NO_CONTENT.into_response(),
        Ok(false) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => repo_error_response(error),
    }
}

fn write_response(outcome: PutOutcome) -> WriteObjectResponse {
    match outcome {
        PutOutcome::Inserted => WriteObjectResponse {
            outcome: "inserted",
            previous_version: None,
            new_version: None,
            expected_version: None,
            actual_version: None,
        },
        PutOutcome::Updated {
            previous_version,
            new_version,
        } => WriteObjectResponse {
            outcome: "updated",
            previous_version: Some(previous_version),
            new_version: Some(new_version),
            expected_version: None,
            actual_version: None,
        },
        PutOutcome::VersionConflict {
            expected_version,
            actual_version,
        } => WriteObjectResponse {
            outcome: "version_conflict",
            previous_version: None,
            new_version: None,
            expected_version: Some(expected_version),
            actual_version: Some(actual_version),
        },
    }
}

fn repo_error_response(error: RepoError) -> Response {
    let status = match error {
        RepoError::NotFound(_) => StatusCode::NOT_FOUND,
        RepoError::InvalidArgument(_) => StatusCode::BAD_REQUEST,
        RepoError::TenantScope(_) => StatusCode::FORBIDDEN,
        RepoError::Backend(_) => StatusCode::INTERNAL_SERVER_ERROR,
    };
    (status, error.to_string()).into_response()
}

fn parse_consistency(value: Option<&str>) -> ReadConsistency {
    match value
        .unwrap_or("strong")
        .trim()
        .to_ascii_lowercase()
        .as_str()
    {
        "eventual" => ReadConsistency::Eventual,
        _ => ReadConsistency::Strong,
    }
}

fn page_from_query(query: &ListQuery) -> Page {
    Page {
        size: query.size.clamp(1, 5_000),
        token: query.token.clone(),
    }
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50101
}

fn default_local_dc() -> String {
    "dc1".to_string()
}

fn default_page_size() -> u32 {
    100
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::{
        body::Body,
        extract::{Path, Query, State},
        http::Request,
    };
    use http_body_util::BodyExt;
    use storage_abstraction::repositories::{
        LinkStore, noop::InMemoryLinkStore, noop::InMemoryObjectStore,
    };
    use tower::util::ServiceExt;

    fn test_state() -> ObjectDatabaseState {
        ObjectDatabaseState {
            objects: Arc::new(InMemoryObjectStore::default()),
            links: Arc::new(InMemoryLinkStore::default()),
            backend: BackendMode::InMemory,
        }
    }

    fn object(
        tenant: &str,
        id: &str,
        type_id: &str,
        owner: Option<&str>,
        markings: Vec<&str>,
        updated_at_ms: i64,
    ) -> Object {
        Object {
            tenant: TenantId(tenant.to_string()),
            id: ObjectId(id.to_string()),
            type_id: TypeId(type_id.to_string()),
            version: 1,
            payload: serde_json::json!({ "id": id }),
            organization_id: None,
            created_at_ms: Some(updated_at_ms),
            updated_at_ms,
            owner: owner.map(|value| OwnerId(value.to_string())),
            markings: markings
                .into_iter()
                .map(|value| MarkingId(value.to_string()))
                .collect(),
        }
    }

    #[tokio::test]
    async fn put_then_get_object_round_trips_through_handler_contract() {
        let state = test_state();
        let write = WriteObjectRequest {
            type_id: "aircraft".to_string(),
            version: 1,
            payload: serde_json::json!({ "tail": "N123OF" }),
            expected_version: None,
            owner: Some("owner-1".to_string()),
            markings: vec!["public".to_string()],
            organization_id: None,
            created_at_ms: Some(1),
            updated_at_ms: Some(2),
        };

        let created = put_object(
            State(state.clone()),
            Path(("tenant-a".to_string(), "object-1".to_string())),
            Json(write),
        )
        .await;
        assert_eq!(created.status(), StatusCode::OK);

        let fetched = get_object(
            State(state),
            Path(("tenant-a".to_string(), "object-1".to_string())),
            Query(ListQuery {
                size: 100,
                token: None,
                consistency: Some("strong".to_string()),
            }),
        )
        .await;
        assert_eq!(fetched.status(), StatusCode::OK);
    }

    #[test]
    fn write_response_preserves_version_conflict_details() {
        let response = write_response(PutOutcome::VersionConflict {
            expected_version: 3,
            actual_version: 4,
        });

        assert_eq!(response.outcome, "version_conflict");
        assert_eq!(response.expected_version, Some(3));
        assert_eq!(response.actual_version, Some(4));
    }

    #[test]
    fn config_splits_contact_points_and_keeps_defaults() {
        let cfg = AppConfig {
            host: default_host(),
            port: default_port(),
            cassandra_contact_points: " 10.0.0.1:9042, ,10.0.0.2:9042 ".to_string(),
            cassandra_local_dc: default_local_dc(),
        };

        assert_eq!(cfg.host, "0.0.0.0");
        assert_eq!(cfg.port, 50101);
        assert_eq!(cfg.cassandra_local_dc, "dc1");
        assert_eq!(
            cfg.cassandra_points(),
            vec!["10.0.0.1:9042".to_string(), "10.0.0.2:9042".to_string()]
        );
    }

    #[tokio::test]
    async fn router_exposes_status_owner_marking_and_links() {
        let objects = Arc::new(InMemoryObjectStore::default());
        objects
            .put(
                object(
                    "tenant-a",
                    "obj-1",
                    "aircraft",
                    Some("owner-1"),
                    vec!["public"],
                    10,
                ),
                None,
            )
            .await
            .expect("seed object");
        objects
            .put(
                object(
                    "tenant-a",
                    "obj-2",
                    "aircraft",
                    Some("owner-2"),
                    vec!["secret"],
                    20,
                ),
                None,
            )
            .await
            .expect("seed object");

        let links = Arc::new(InMemoryLinkStore::default());
        links
            .put(Link {
                tenant: TenantId("tenant-a".to_string()),
                link_type: LinkTypeId("owns".to_string()),
                from: ObjectId("obj-1".to_string()),
                to: ObjectId("obj-2".to_string()),
                payload: Some(serde_json::json!({ "rank": 1 })),
                created_at_ms: 30,
            })
            .await
            .expect("seed link");

        let app = build_router(ObjectDatabaseState {
            objects,
            links,
            backend: BackendMode::InMemory,
        });

        let status = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri("/status")
                    .body(Body::empty())
                    .expect("status request"),
            )
            .await
            .expect("status response");
        assert_eq!(status.status(), StatusCode::OK);
        let status_body = status.into_body().collect().await.expect("body").to_bytes();
        let status_json: Value = serde_json::from_slice(&status_body).expect("json");
        assert_eq!(status_json["ready"], true);
        assert_eq!(status_json["backend"], "in_memory");

        let ready = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri("/ready")
                    .body(Body::empty())
                    .expect("ready request"),
            )
            .await
            .expect("ready response");
        assert_eq!(ready.status(), StatusCode::OK);

        let by_owner = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri("/api/v1/object-database/objects/tenant-a/by-owner/owner-1?size=10")
                    .body(Body::empty())
                    .expect("owner request"),
            )
            .await
            .expect("owner response");
        assert_eq!(by_owner.status(), StatusCode::OK);
        let body = by_owner
            .into_body()
            .collect()
            .await
            .expect("body")
            .to_bytes();
        let json: Value = serde_json::from_slice(&body).expect("json");
        assert_eq!(json["items"].as_array().map(Vec::len), Some(1));
        assert_eq!(json["items"][0]["id"], "obj-1");

        let by_marking = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri("/api/v1/object-database/objects/tenant-a/by-marking/secret?size=10")
                    .body(Body::empty())
                    .expect("marking request"),
            )
            .await
            .expect("marking response");
        assert_eq!(by_marking.status(), StatusCode::OK);
        let body = by_marking
            .into_body()
            .collect()
            .await
            .expect("body")
            .to_bytes();
        let json: Value = serde_json::from_slice(&body).expect("json");
        assert_eq!(json["items"].as_array().map(Vec::len), Some(1));
        assert_eq!(json["items"][0]["id"], "obj-2");

        let outgoing = app
            .clone()
            .oneshot(
                Request::builder()
                    .uri("/api/v1/object-database/links/tenant-a/owns/outgoing/obj-1?size=10")
                    .body(Body::empty())
                    .expect("links request"),
            )
            .await
            .expect("links response");
        assert_eq!(outgoing.status(), StatusCode::OK);
        let body = outgoing
            .into_body()
            .collect()
            .await
            .expect("body")
            .to_bytes();
        let json: Value = serde_json::from_slice(&body).expect("json");
        assert_eq!(json["items"].as_array().map(Vec::len), Some(1));
        assert_eq!(json["items"][0]["from"], "obj-1");
        assert_eq!(json["items"][0]["to"], "obj-2");

        let incoming = app
            .oneshot(
                Request::builder()
                    .uri("/api/v1/object-database/links/tenant-a/owns/incoming/obj-2?size=10")
                    .body(Body::empty())
                    .expect("incoming links request"),
            )
            .await
            .expect("incoming links response");
        assert_eq!(incoming.status(), StatusCode::OK);
        let body = incoming
            .into_body()
            .collect()
            .await
            .expect("body")
            .to_bytes();
        let json: Value = serde_json::from_slice(&body).expect("json");
        assert_eq!(json["items"].as_array().map(Vec::len), Some(1));
        assert_eq!(json["items"][0]["from"], "obj-1");
        assert_eq!(json["items"][0]["to"], "obj-2");
    }
}
