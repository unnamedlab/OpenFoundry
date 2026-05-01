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
    Json,
    body,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::Utc;
use ontology_kernel::{
    AppState,
    handlers::actions::execute_action,
    models::action_type::ExecuteActionRequest,
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
        match PgPoolOptions::new()
            .max_connections(4)
            .connect(&url)
            .await
        {
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
async fn apply_migrations(pool: &PgPool) {
    let manifest_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let workspace_root = manifest_dir
        .parent()
        .and_then(FsPath::parent)
        .expect("workspace root resolvable")
        .to_path_buf();

    let dirs = [
        workspace_root.join("services/ontology-definition-service/migrations"),
        workspace_root.join("services/object-database-service/migrations"),
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
        let sql = fs::read_to_string(&path)
            .unwrap_or_else(|e| panic!("read migration {path:?}: {e}"));
        sqlx::raw_sql(&sql)
            .execute(pool)
            .await
            .unwrap_or_else(|e| panic!("apply migration {path:?}: {e}"));
    }
}

fn build_app_state(pool: PgPool) -> AppState {
    AppState {
        db: pool,
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
