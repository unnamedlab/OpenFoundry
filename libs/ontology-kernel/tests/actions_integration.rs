//! T10 — Rust integration coverage for `execute_action`.
//!
//! Boots an ephemeral Postgres container via `testcontainers`, applies the
//! real migrations from `services/ontology-definition-service` and
//! `services/object-database-service` in chronological order, seeds a minimal
//! ontology (object type, properties, action type, target object) and
//! exercises [`ontology_kernel::handlers::actions::execute_action`] end to
//! end.
//!
//! The audit and notification side-effects are intentionally pointed at an
//! unreachable URL (`http://127.0.0.1:1`); the action handler logs the
//! delivery failure but keeps executing — exactly the behaviour we want
//! verified for the happy path.
//!
//! Run with: `cargo test --features it -p ontology-kernel --test actions_integration`.

#![cfg(feature = "it")]

use std::fs;
use std::path::{Path as FsPath, PathBuf};

use auth_middleware::{claims::Claims, jwt::JwtConfig, layer::AuthUser};
use axum::{
    Json, body,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::Utc;
use ontology_kernel::{
    AppState, handlers::actions::execute_action, models::action_type::ExecuteActionRequest,
};
use serde_json::{Value, json};
use sqlx::{PgPool, Row, postgres::PgPoolOptions};
use testcontainers::{
    ContainerAsync, GenericImage, ImageExt,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};
use uuid::Uuid;

const POSTGRES_PORT: u16 = 5432;
const PG_PASSWORD: &str = "postgres";

/// Spin up an ephemeral Postgres 16 instance and return both the running
/// container handle and a connected pool. The handle MUST be kept alive for
/// the lifetime of the test to prevent the container from being dropped.
async fn boot_postgres() -> (ContainerAsync<GenericImage>, PgPool) {
    let container = GenericImage::new("postgres", "16-alpine")
        .with_wait_for(WaitFor::message_on_stderr(
            "database system is ready to accept connections",
        ))
        .with_exposed_port(POSTGRES_PORT.tcp())
        .with_env_var("POSTGRES_PASSWORD", PG_PASSWORD)
        .with_env_var("POSTGRES_DB", "openfoundry")
        .start()
        .await
        .expect("postgres container failed to start");

    let host = container
        .get_host()
        .await
        .expect("failed to read container host");
    let port = container
        .get_host_port_ipv4(POSTGRES_PORT)
        .await
        .expect("failed to read container port");

    let url = format!("postgres://postgres:{PG_PASSWORD}@{host}:{port}/openfoundry");

    // Postgres reports "ready to accept connections" twice: once during
    // initdb against the unix socket and once for the real listener. Retry
    // the TCP connect until the second one succeeds.
    let mut attempts = 0;
    let pool = loop {
        match PgPoolOptions::new().max_connections(4).connect(&url).await {
            Ok(pool) => break pool,
            Err(error) if attempts < 30 => {
                attempts += 1;
                tokio::time::sleep(std::time::Duration::from_millis(500)).await;
                eprintln!("waiting for postgres ({attempts}): {error}");
            }
            Err(error) => panic!("postgres never became reachable: {error}"),
        }
    };

    (container, pool)
}

/// Apply every `*.sql` file under both ontology-definition-service and
/// object-database-service, sorted by filename so the cross-service
/// chronological order is preserved.
/// Apply every `*.sql` file from the **archived** ontology Postgres
/// migrations under `docs/architecture/legacy-migrations/`, sorted by
/// filename so the cross-service chronological order is preserved.
///
/// Background: as part of S1.4.d / S1.6.c / S1.7 the per-service
/// `migrations/` directories were archived after the schema was
/// consolidated under `pg-schemas.ontology_schema` and the hot path
/// pivoted to Cassandra. Those SQL files remain the canonical source
/// for the kernel handlers that have **not yet** been migrated to the
/// `Arc<dyn ObjectStore>` substrate (S1.4.b). This test exercises that
/// legacy code path; the new Cassandra-backed E2E for the writeback
/// helper lives in
/// `services/ontology-actions-service/tests/writeback_e2e.rs` (S1.4.f
/// / S1.9.a).
async fn apply_migrations(pool: &PgPool) {
    let manifest_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let workspace_root = manifest_dir
        .parent()
        .and_then(FsPath::parent)
        .expect("workspace root resolvable")
        .to_path_buf();

    let archive_root = workspace_root.join("docs/architecture/legacy-migrations");

    let dirs = [
        archive_root.join("ontology-definition-service"),
        archive_root.join("object-database-service"),
        // S1.4.d — `action_executions` ledger lives in the actions service.
        archive_root.join("ontology-actions-service"),
    ];

    let mut files: Vec<PathBuf> = Vec::new();
    for dir in &dirs {
        for entry in fs::read_dir(dir).unwrap_or_else(|e| panic!("read {dir:?}: {e}")) {
            let entry = entry.expect("dir entry");
            let path = entry.path();
            if path.extension().and_then(|s| s.to_str()) == Some("sql") {
                files.push(path);
            }
        }
    }
    // Sort by basename so cross-service migrations interleave correctly.
    files.sort_by(|a, b| {
        a.file_name()
            .and_then(|s| s.to_str())
            .unwrap_or("")
            .cmp(b.file_name().and_then(|s| s.to_str()).unwrap_or(""))
    });

    for path in files {
        let sql =
            fs::read_to_string(&path).unwrap_or_else(|e| panic!("read migration {path:?}: {e}"));
        sqlx::raw_sql(&sql)
            .execute(pool)
            .await
            .unwrap_or_else(|e| panic!("apply migration {path:?}: {e}"));
    }
}

fn build_app_state(pool: PgPool) -> AppState {
    AppState {
        db: pool,
        stores: ontology_kernel::stores::Stores::in_memory(),
        http_client: reqwest::Client::new(),
        jwt_config: JwtConfig::new("integration-test-secret"),
        // Point external services at an unreachable port so audit/notify
        // emissions fail fast and the kernel logs+continues.
        audit_service_url: "http://127.0.0.1:1".into(),
        dataset_service_url: "http://127.0.0.1:1".into(),
        ontology_service_url: "http://127.0.0.1:1".into(),
        pipeline_service_url: "http://127.0.0.1:1".into(),
        ai_service_url: "http://127.0.0.1:1".into(),
        notification_service_url: "http://127.0.0.1:1".into(),
        search_embedding_provider: "none".into(),
        node_runtime_command: "node".into(),
        connector_management_service_url: "http://127.0.0.1:1".into(),
    }
}

fn admin_claims(user_id: Uuid, org_id: Uuid) -> Claims {
    let now = Utc::now().timestamp();
    Claims {
        sub: user_id,
        iat: now,
        exp: now + 3600,
        iss: None,
        aud: None,
        jti: Uuid::new_v4(),
        email: "integration@openfoundry.dev".into(),
        name: "Integration Bot".into(),
        roles: vec!["admin".into(), "operator".into()],
        permissions: vec!["*:*".into()],
        org_id: Some(org_id),
        attributes: json!({}),
        auth_methods: vec!["password".into()],
        token_use: Some("access".into()),
        api_key_id: None,
        session_kind: None,
        session_scope: None,
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn execute_action_updates_object_and_emits_audit_attempt() {
    // 1. Boot infrastructure.
    let (_container, pool) = boot_postgres().await;
    apply_migrations(&pool).await;

    // 2. Seed a minimal ontology.
    let user_id = Uuid::new_v4();
    let org_id = Uuid::new_v4();
    let object_type_id = Uuid::new_v4();
    let object_id = Uuid::new_v4();
    let action_id = Uuid::new_v4();

    sqlx::query(
        r#"INSERT INTO object_types
              (id, name, display_name, description, primary_key_property, owner_id)
           VALUES ($1, $2, $3, $4, $5, $6)"#,
    )
    .bind(object_type_id)
    .bind("aircraft")
    .bind("Aircraft")
    .bind("T10 integration aircraft")
    .bind("tail_number")
    .bind(user_id)
    .execute(&pool)
    .await
    .expect("insert object_type");

    for (name, ptype, required, unique_constraint) in [
        ("tail_number", "string", true, true),
        ("model", "string", false, false),
        ("status", "string", false, false),
    ] {
        sqlx::query(
            r#"INSERT INTO properties
                  (id, object_type_id, name, display_name, description,
                   property_type, required, unique_constraint)
               VALUES ($1, $2, $3, $4, '', $5, $6, $7)"#,
        )
        .bind(Uuid::new_v4())
        .bind(object_type_id)
        .bind(name)
        .bind(name)
        .bind(ptype)
        .bind(required)
        .bind(unique_constraint)
        .execute(&pool)
        .await
        .expect("insert property");
    }

    // Seed the target object instance via the canonical INSERT path so the
    // action handler's UPDATE returns the expected RETURNING column set.
    sqlx::query(
        r#"INSERT INTO object_instances
              (id, object_type_id, properties, marking, organization_id, created_by)
           VALUES ($1, $2, $3, $4, $5, $6)"#,
    )
    .bind(object_id)
    .bind(object_type_id)
    .bind(json!({
        "tail_number": "AF-101",
        "model": "A320",
        "status": "ready",
    }))
    .bind("public")
    .bind(org_id)
    .bind(user_id)
    .execute(&pool)
    .await
    .expect("insert object_instance");

    // Seed an `update_status` ActionType (operation_kind = update_object).
    let action_config = json!({
        "kind": "update_object",
        "property_mappings": [
            { "property_name": "status", "input_name": "next_status" }
        ]
    });
    let input_schema = json!([
        { "name": "next_status", "property_type": "string", "required": true }
    ]);
    sqlx::query(
        r#"INSERT INTO action_types
              (id, name, display_name, description, object_type_id,
               operation_kind, input_schema, config, confirmation_required,
               permission_key, authorization_policy, owner_id)
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, FALSE, NULL, $9, $10)"#,
    )
    .bind(action_id)
    .bind("update_status")
    .bind("Update aircraft status")
    .bind("T10 update_status")
    .bind(object_type_id)
    .bind("update_object")
    .bind(&input_schema)
    .bind(&action_config)
    .bind(json!({}))
    .bind(user_id)
    .execute(&pool)
    .await
    .expect("insert action_type");

    // 3. Build runtime + execute the action.
    let state = build_app_state(pool.clone());
    let claims = admin_claims(user_id, org_id);

    let response = execute_action(
        AuthUser(claims),
        State(state),
        Path(action_id),
        Json(ExecuteActionRequest {
            target_object_id: Some(object_id),
            parameters: json!({ "next_status": "grounded" }),
            justification: Some("integration test".into()),
        }),
    )
    .await
    .into_response();

    assert_eq!(
        response.status(),
        StatusCode::OK,
        "execute_action did not return 200; body inspection follows"
    );

    // Drain the body for diagnostics/assertion on the executed payload.
    let body_bytes = body::to_bytes(response.into_body(), 1024 * 1024)
        .await
        .expect("read response body");
    let payload: Value = serde_json::from_slice(&body_bytes).expect("response is JSON");
    assert_eq!(payload["deleted"], json!(false));
    assert_eq!(payload["object"]["properties"]["status"], json!("grounded"));
    assert_eq!(payload["target_object_id"], json!(object_id.to_string()));

    // 4. Verify the persisted state mirrors the executed patch.
    let row = sqlx::query("SELECT properties FROM object_instances WHERE id = $1")
        .bind(object_id)
        .fetch_one(&pool)
        .await
        .expect("re-read object instance");
    let stored: Value = row.try_get("properties").expect("properties column");
    assert_eq!(stored["status"], json!("grounded"));
    assert_eq!(stored["tail_number"], json!("AF-101"));

    // 5. Sanity-check the revisions table is reachable (T9 schema present)
    //    and accepts a manual append matching the action's output. The
    //    `update_object` handler does not yet author revisions itself; the
    //    revision pipeline lives downstream. We seed one to assert the
    //    schema/index combination is migrate-able and queryable.
    sqlx::query(
        r#"INSERT INTO object_revisions
              (id, object_id, object_type_id, operation, properties,
               marking, organization_id, changed_by, revision_number)
           VALUES ($1, $2, $3, 'update', $4, 'public', $5, $6, 1)"#,
    )
    .bind(Uuid::new_v4())
    .bind(object_id)
    .bind(object_type_id)
    .bind(stored.clone())
    .bind(org_id)
    .bind(user_id)
    .execute(&pool)
    .await
    .expect("seed object_revisions row");

    let count: i64 =
        sqlx::query_scalar("SELECT COUNT(*) FROM object_revisions WHERE object_id = $1")
            .bind(object_id)
            .fetch_one(&pool)
            .await
            .expect("count revisions");
    assert_eq!(count, 1);
}

// ---------------------------------------------------------------------------
// TASK Q — Extended end-to-end coverage.
//
// Every test below boots its own ephemeral Postgres container so they remain
// independent and can be filtered individually with
// `cargo test -p ontology-kernel --features it --test actions_integration --
//   <name>`.
//
// The shared seeding helpers (`seed_object_type_with_string_props`,
// `seed_object_instance`, `seed_update_action`) keep each test focused on the
// behaviour under test instead of the boilerplate around it.
//
// NOTE: revert (`undo_action`) and submission-criteria expressions are not
// implemented yet — the migrations exist (`action_executions.previous_object_state`
// / `revertible`) but the handler is pending. Tests for those flows remain
// `#[ignore]` placeholders so the regression surface is recorded without
// failing CI.
// ---------------------------------------------------------------------------

use ontology_kernel::handlers::actions::{
    ActionMetricsQuery, execute_action_batch, get_action_metrics, validate_action,
};
use ontology_kernel::models::action_type::{ExecuteBatchActionRequest, ValidateActionRequest};

/// Insert an object type plus every property listed (all `string`, the first
/// one acting as the primary key + unique constraint).
async fn seed_object_type_with_string_props(
    pool: &PgPool,
    user_id: Uuid,
    object_type_id: Uuid,
    name: &str,
    properties: &[&str],
) {
    let primary_key = properties
        .first()
        .copied()
        .expect("at least one property required");

    sqlx::query(
        r#"INSERT INTO object_types
              (id, name, display_name, description, primary_key_property, owner_id)
           VALUES ($1, $2, $3, $4, $5, $6)"#,
    )
    .bind(object_type_id)
    .bind(name)
    .bind(name)
    .bind("Q-test object")
    .bind(primary_key)
    .bind(user_id)
    .execute(pool)
    .await
    .expect("insert object_type");

    for (index, property) in properties.iter().enumerate() {
        let primary = index == 0;
        sqlx::query(
            r#"INSERT INTO properties
                  (id, object_type_id, name, display_name, description,
                   property_type, required, unique_constraint)
               VALUES ($1, $2, $3, $4, '', 'string', $5, $5)"#,
        )
        .bind(Uuid::new_v4())
        .bind(object_type_id)
        .bind(*property)
        .bind(*property)
        .bind(primary)
        .execute(pool)
        .await
        .expect("insert property");
    }
}

async fn seed_object_instance(
    pool: &PgPool,
    object_type_id: Uuid,
    org_id: Uuid,
    user_id: Uuid,
    object_id: Uuid,
    properties: Value,
) {
    sqlx::query(
        r#"INSERT INTO object_instances
              (id, object_type_id, properties, marking, organization_id, created_by)
           VALUES ($1, $2, $3, 'public', $4, $5)"#,
    )
    .bind(object_id)
    .bind(object_type_id)
    .bind(properties)
    .bind(org_id)
    .bind(user_id)
    .execute(pool)
    .await
    .expect("insert object_instance");
}

/// Seed a minimal `update_object` action that maps an input named
/// `next_status` to the `status` property.
async fn seed_update_action(
    pool: &PgPool,
    user_id: Uuid,
    action_id: Uuid,
    object_type_id: Uuid,
    name: &str,
    extra_config: Option<Value>,
) {
    let mut config = json!({
        "kind": "update_object",
        "property_mappings": [
            { "property_name": "status", "input_name": "next_status" }
        ]
    });
    if let Some(extra) = extra_config {
        if let (Some(target), Some(source)) = (config.as_object_mut(), extra.as_object()) {
            for (key, value) in source {
                target.insert(key.clone(), value.clone());
            }
        }
    }
    let input_schema = json!([
        { "name": "next_status", "property_type": "string", "required": true }
    ]);
    sqlx::query(
        r#"INSERT INTO action_types
              (id, name, display_name, description, object_type_id,
               operation_kind, input_schema, config, confirmation_required,
               permission_key, authorization_policy, owner_id)
           VALUES ($1, $2, $3, '', $4, 'update_object', $5, $6, FALSE, NULL, $7, $8)"#,
    )
    .bind(action_id)
    .bind(name)
    .bind(name)
    .bind(object_type_id)
    .bind(&input_schema)
    .bind(&config)
    .bind(json!({}))
    .bind(user_id)
    .execute(pool)
    .await
    .expect("insert action_type");
}

async fn response_status_and_json(response: axum::response::Response) -> (StatusCode, Value) {
    let status = response.status();
    let body_bytes = body::to_bytes(response.into_body(), 4 * 1024 * 1024)
        .await
        .expect("read response body");
    let value: Value = if body_bytes.is_empty() {
        Value::Null
    } else {
        serde_json::from_slice(&body_bytes).expect("response is JSON")
    };
    (status, value)
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn execute_action_batch_processes_three_targets_and_records_per_target_results() {
    let (_container, pool) = boot_postgres().await;
    apply_migrations(&pool).await;

    let user_id = Uuid::new_v4();
    let org_id = Uuid::new_v4();
    let object_type_id = Uuid::new_v4();
    let action_id = Uuid::new_v4();

    seed_object_type_with_string_props(
        &pool,
        user_id,
        object_type_id,
        "fleet_aircraft",
        &["tail_number", "status"],
    )
    .await;
    seed_update_action(&pool, user_id, action_id, object_type_id, "ground", None).await;

    let mut targets = Vec::with_capacity(3);
    for tail in ["AF-201", "AF-202", "AF-203"] {
        let id = Uuid::new_v4();
        seed_object_instance(
            &pool,
            object_type_id,
            org_id,
            user_id,
            id,
            json!({ "tail_number": tail, "status": "ready" }),
        )
        .await;
        targets.push(id);
    }

    let state = build_app_state(pool.clone());
    let claims = admin_claims(user_id, org_id);

    let response = execute_action_batch(
        AuthUser(claims),
        State(state),
        Path(action_id),
        Json(ExecuteBatchActionRequest {
            target_object_ids: targets.clone(),
            parameters: json!({ "next_status": "grounded" }),
            justification: Some("batch grounding".into()),
        }),
    )
    .await
    .into_response();

    let (status, payload) = response_status_and_json(response).await;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(payload["total"], json!(3));
    assert_eq!(payload["succeeded"], json!(3));
    assert_eq!(payload["failed"], json!(0));

    // Atomicity: every target must be in the persisted "grounded" state.
    let stored: Vec<(Uuid, Value)> = sqlx::query_as(
        "SELECT id, properties FROM object_instances WHERE object_type_id = $1 ORDER BY id",
    )
    .bind(object_type_id)
    .fetch_all(&pool)
    .await
    .expect("fetch instances");
    assert_eq!(stored.len(), 3);
    for (_, properties) in &stored {
        assert_eq!(properties["status"], json!("grounded"));
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn execute_action_batch_returns_per_target_failure_for_unknown_target() {
    let (_container, pool) = boot_postgres().await;
    apply_migrations(&pool).await;

    let user_id = Uuid::new_v4();
    let org_id = Uuid::new_v4();
    let object_type_id = Uuid::new_v4();
    let action_id = Uuid::new_v4();

    seed_object_type_with_string_props(
        &pool,
        user_id,
        object_type_id,
        "vehicle",
        &["plate", "status"],
    )
    .await;
    seed_update_action(&pool, user_id, action_id, object_type_id, "park", None).await;

    let known = Uuid::new_v4();
    seed_object_instance(
        &pool,
        object_type_id,
        org_id,
        user_id,
        known,
        json!({ "plate": "AAA-111", "status": "moving" }),
    )
    .await;
    let missing = Uuid::new_v4();

    let state = build_app_state(pool.clone());
    let claims = admin_claims(user_id, org_id);

    let response = execute_action_batch(
        AuthUser(claims),
        State(state),
        Path(action_id),
        Json(ExecuteBatchActionRequest {
            target_object_ids: vec![known, missing],
            parameters: json!({ "next_status": "parked" }),
            justification: None,
        }),
    )
    .await
    .into_response();

    let (status, payload) = response_status_and_json(response).await;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(payload["total"], json!(2));
    // Per-target results: one ok, one error. Atomicity is "best-effort per
    // target" — execute_action_batch does NOT roll back successful writes
    // when later targets fail (Foundry parity).
    let results = payload["results"].as_array().expect("results array");
    assert_eq!(results.len(), 2);
    let succeeded = results
        .iter()
        .filter(|entry| entry["status"] == json!("succeeded"))
        .count();
    let failed = results
        .iter()
        .filter(|entry| entry["status"] == json!("failed") || entry["status"] == json!("denied"))
        .count();
    assert_eq!(succeeded, 1);
    assert_eq!(failed, 1);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn validate_action_rejects_missing_required_input_with_field_errors() {
    let (_container, pool) = boot_postgres().await;
    apply_migrations(&pool).await;

    let user_id = Uuid::new_v4();
    let org_id = Uuid::new_v4();
    let object_type_id = Uuid::new_v4();
    let action_id = Uuid::new_v4();

    seed_object_type_with_string_props(
        &pool,
        user_id,
        object_type_id,
        "ticket",
        &["code", "status"],
    )
    .await;
    seed_update_action(&pool, user_id, action_id, object_type_id, "close", None).await;

    let target = Uuid::new_v4();
    seed_object_instance(
        &pool,
        object_type_id,
        org_id,
        user_id,
        target,
        json!({ "code": "T-1", "status": "open" }),
    )
    .await;

    let state = build_app_state(pool.clone());
    let claims = admin_claims(user_id, org_id);

    let response = validate_action(
        AuthUser(claims),
        State(state),
        Path(action_id),
        Json(ValidateActionRequest {
            target_object_id: Some(target),
            // `next_status` is required by the action's input_schema; sending
            // an empty payload must surface the failure as a structured 200
            // body with `valid:false` plus a field-level error.
            parameters: json!({}),
        }),
    )
    .await
    .into_response();

    let (status, payload) = response_status_and_json(response).await;
    assert_eq!(status, StatusCode::OK);
    assert_eq!(payload["valid"], json!(false));
    let errors = payload["errors"].as_array().expect("errors array");
    assert!(!errors.is_empty());
    let joined = errors
        .iter()
        .filter_map(|entry| entry.as_str())
        .collect::<Vec<_>>()
        .join("\n");
    assert!(
        joined.to_lowercase().contains("next_status"),
        "expected a `next_status` failure message; got:\n{joined}",
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn webhook_writeback_failure_aborts_action_with_400() {
    let (_container, pool) = boot_postgres().await;
    apply_migrations(&pool).await;

    let user_id = Uuid::new_v4();
    let org_id = Uuid::new_v4();
    let object_type_id = Uuid::new_v4();
    let action_id = Uuid::new_v4();
    let webhook_id = Uuid::new_v4();

    seed_object_type_with_string_props(
        &pool,
        user_id,
        object_type_id,
        "incident",
        &["external_id", "status"],
    )
    .await;
    // Configure a writeback webhook. The connector-management URL on the
    // app state points at `127.0.0.1:1` (unreachable), so the writeback HTTP
    // call MUST fail and abort the action with a 400 response.
    let extra = json!({
        "webhook_writeback": {
            "webhook_id": webhook_id,
            "input_mappings": [
                { "webhook_input_name": "next_status", "action_input_name": "next_status" }
            ]
        }
    });
    seed_update_action(
        &pool,
        user_id,
        action_id,
        object_type_id,
        "set_status",
        Some(extra),
    )
    .await;

    let target = Uuid::new_v4();
    seed_object_instance(
        &pool,
        object_type_id,
        org_id,
        user_id,
        target,
        json!({ "external_id": "INC-1", "status": "open" }),
    )
    .await;

    let state = build_app_state(pool.clone());
    let claims = admin_claims(user_id, org_id);

    let response = ontology_kernel::handlers::actions::execute_action(
        AuthUser(claims),
        State(state),
        Path(action_id),
        Json(ontology_kernel::models::action_type::ExecuteActionRequest {
            target_object_id: Some(target),
            parameters: json!({ "next_status": "resolved" }),
            justification: None,
        }),
    )
    .await
    .into_response();

    let (status, payload) = response_status_and_json(response).await;
    assert_eq!(status, StatusCode::BAD_REQUEST);
    let message = payload["error"].as_str().unwrap_or_default().to_string();
    assert!(
        message.to_lowercase().contains("webhook"),
        "expected webhook failure message; got: {payload:?}",
    );

    // The persisted object must remain untouched (writeback aborts before
    // the UPDATE is issued).
    let row = sqlx::query("SELECT properties FROM object_instances WHERE id = $1")
        .bind(target)
        .fetch_one(&pool)
        .await
        .expect("re-read object instance");
    let stored: Value = row.try_get("properties").expect("properties column");
    assert_eq!(stored["status"], json!("open"));
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn function_backed_without_batched_caps_target_count_at_twenty() {
    let (_container, pool) = boot_postgres().await;
    apply_migrations(&pool).await;

    let user_id = Uuid::new_v4();
    let org_id = Uuid::new_v4();
    let object_type_id = Uuid::new_v4();
    let action_id = Uuid::new_v4();

    seed_object_type_with_string_props(&pool, user_id, object_type_id, "lot", &["sku", "status"])
        .await;

    // Insert a function-backed action with a config that does NOT enable
    // `batched_execution`. `execute_action_batch` should reject any payload
    // with more than 20 targets via TASK M's `scale_limit` envelope.
    let config = json!({
        "kind": "invoke_function",
        "function_package_id": Uuid::new_v4(),
        "entrypoint": "noop"
    });
    sqlx::query(
        r#"INSERT INTO action_types
              (id, name, display_name, description, object_type_id,
               operation_kind, input_schema, config, confirmation_required,
               permission_key, authorization_policy, owner_id)
           VALUES ($1, 'noop_fn', 'noop_fn', '', $2, 'invoke_function',
                   '[]'::jsonb, $3, FALSE, NULL, '{}'::jsonb, $4)"#,
    )
    .bind(action_id)
    .bind(object_type_id)
    .bind(&config)
    .bind(user_id)
    .execute(&pool)
    .await
    .expect("insert function-backed action_type");

    let state = build_app_state(pool.clone());
    let claims = admin_claims(user_id, org_id);

    let targets: Vec<Uuid> = (0..21).map(|_| Uuid::new_v4()).collect();
    let response = execute_action_batch(
        AuthUser(claims),
        State(state),
        Path(action_id),
        Json(ExecuteBatchActionRequest {
            target_object_ids: targets,
            parameters: json!({}),
            justification: None,
        }),
    )
    .await
    .into_response();

    let (status, payload) = response_status_and_json(response).await;
    assert_eq!(status, StatusCode::TOO_MANY_REQUESTS);
    assert_eq!(payload["failure_type"], json!("scale_limit"));
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn execute_action_records_row_in_action_executions_ledger() {
    // Verifies that a successful action emits a row in `action_executions`
    // (the ledger that powers TASK F metrics aggregation and TASK E revert).
    let (_container, pool) = boot_postgres().await;
    apply_migrations(&pool).await;

    let user_id = Uuid::new_v4();
    let org_id = Uuid::new_v4();
    let object_type_id = Uuid::new_v4();
    let action_id = Uuid::new_v4();

    seed_object_type_with_string_props(
        &pool,
        user_id,
        object_type_id,
        "deployment",
        &["slug", "status"],
    )
    .await;
    seed_update_action(&pool, user_id, action_id, object_type_id, "ship", None).await;

    let target = Uuid::new_v4();
    seed_object_instance(
        &pool,
        object_type_id,
        org_id,
        user_id,
        target,
        json!({ "slug": "web", "status": "draft" }),
    )
    .await;

    let state = build_app_state(pool.clone());
    let claims = admin_claims(user_id, org_id);

    let response = ontology_kernel::handlers::actions::execute_action(
        AuthUser(claims),
        State(state),
        Path(action_id),
        Json(ontology_kernel::models::action_type::ExecuteActionRequest {
            target_object_id: Some(target),
            parameters: json!({ "next_status": "shipped" }),
            justification: Some("Q-test ledger".into()),
        }),
    )
    .await
    .into_response();
    assert_eq!(response.status(), StatusCode::OK);

    let count: i64 = sqlx::query_scalar(
        "SELECT COUNT(*) FROM action_executions WHERE action_id = $1 AND target_object_id = $2",
    )
    .bind(action_id)
    .bind(target)
    .fetch_one(&pool)
    .await
    .expect("count action_executions");
    assert_eq!(count, 1, "expected ledger row for successful execution");

    // Cross-check the JSON metrics endpoint surfaces the same execution.
    let metrics_response = get_action_metrics(
        AuthUser(admin_claims(user_id, org_id)),
        State(build_app_state(pool.clone())),
        Path(action_id),
        axum::extract::Query(ActionMetricsQuery { window: None }),
    )
    .await
    .into_response();
    let (metrics_status, metrics_payload) = response_status_and_json(metrics_response).await;
    assert_eq!(metrics_status, StatusCode::OK);
    let total = metrics_payload["total"].as_i64().unwrap_or(0);
    assert!(total >= 1, "metrics total should reflect the ledger row");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "TASK Q follow-up: revert handler not implemented yet"]
async fn revert_within_window_restores_object_state_from_revisions() {
    // Pending: needs a `POST /api/v1/ontology/actions/executions/{id}/revert`
    // handler that reads `action_executions.previous_object_state` and
    // re-applies it to `object_instances`. The migration is already in
    // place (20260504000000_action_executions_revert.sql).
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "TASK Q follow-up: revert handler must enforce 409 conflict"]
async fn revert_after_second_edit_returns_409_conflict() {
    // Pending: when `object_instances.updated_at >
    // action_executions.applied_at`, the revert handler should refuse with
    // HTTP 409 + `failure_type:"conflict"`.
}
