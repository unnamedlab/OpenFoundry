//! S1.4.f end-to-end test for the Cassandra writeback pattern.
//!
//! Boots **Cassandra 5.0.2** and **Postgres 16** via `testcontainers`,
//! provisions the production `ontology_objects.*` DDL plus the
//! `outbox.events` schema owned by `libs/outbox/migrations/`, then
//! drives 1 000 concurrent writes through
//! [`ontology_kernel::domain::writeback::apply_object_with_outbox`].
//!
//! Asserts:
//!
//! * Every primary write landed in `objects_by_id` (count = 1 000).
//! * Every event id is unique and deterministic from
//!   `(tenant, aggregate, aggregate_id, version)`.
//! * The outbox table is **empty in steady state** because
//!   [`outbox::enqueue`] does INSERT+DELETE in the same transaction
//!   (the WAL still carries both records — Debezium picks them up
//!   in production, validated separately by
//!   `libs/outbox/tests/e2e_debezium.sh`).
//! * A retry of an already-applied write hits the
//!   `idempotent_retry = true` branch instead of failing.
//!
//! Marked `#[ignore]` because CI runners without Docker would
//! otherwise fail. Run locally with:
//!
//! ```text
//! cargo test -p ontology-actions-service --test writeback_e2e -- --include-ignored
//! ```

use std::sync::Arc;
use std::time::Duration;

use cassandra_kernel::{ClusterConfig, SessionBuilder, repos::CassandraObjectStore};
use futures::{StreamExt, stream};
use ontology_kernel::domain::writeback::{
    WritebackOutcome, apply_object_with_outbox, derive_event_id,
};
use sqlx::{PgPool, postgres::PgPoolOptions};
use storage_abstraction::repositories::{MarkingId, Object, ObjectId, OwnerId, TenantId, TypeId};
use testcontainers::{
    ContainerAsync, GenericImage, ImageExt,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};
use uuid::Uuid;

const POSTGRES_PORT: u16 = 5432;
const PG_PASSWORD: &str = "postgres";
const TOTAL: usize = 1_000;
const PARALLELISM: usize = 64;

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires Docker; run with --include-ignored"]
async fn writeback_applies_1000_actions_concurrently() {
    let _ = tracing_subscriber::fmt::try_init();

    // -- 1. Boot infrastructure ----------------------------------------
    let (_pg_container, pg_pool) = boot_postgres().await;
    let (_cas_container, cassandra) = boot_cassandra().await;

    let object_store = Arc::new(CassandraObjectStore::new(cassandra.clone()));
    object_store.warm_up().await.expect("warm-up");

    // -- 2. Drive 1 000 concurrent writebacks --------------------------
    let tenant = TenantId(format!("acme-{}", Uuid::new_v4()));
    let owner = OwnerId(Uuid::new_v4().to_string());
    let now_ms = chrono::Utc::now().timestamp_millis();

    let mut object_ids = Vec::with_capacity(TOTAL);
    for _ in 0..TOTAL {
        object_ids.push(ObjectId(Uuid::now_v7().to_string()));
    }

    let outcomes: Vec<WritebackOutcome> =
        stream::iter(object_ids.iter().enumerate().map(|(i, id)| {
            let pg = pg_pool.clone();
            let store = object_store.clone();
            let object = Object {
                tenant: tenant.clone(),
                id: id.clone(),
                type_id: TypeId(format!("type_{}", i % 10)),
                version: 0,
                payload: serde_json::json!({"i": i}),
                organization_id: None,
                created_at_ms: Some(now_ms + i as i64),
                updated_at_ms: now_ms + i as i64,
                owner: Some(owner.clone()),
                markings: vec![MarkingId("PUBLIC".to_string())],
            };
            async move {
                apply_object_with_outbox(
                    &pg,
                    store.as_ref(),
                    object,
                    None,
                    "object",
                    "ontology.object.changed.v1",
                    serde_json::json!({"i": i}),
                )
                .await
                .expect("writeback succeeds")
            }
        }))
        .buffer_unordered(PARALLELISM)
        .collect()
        .await;

    // -- 3. Assertions -------------------------------------------------
    assert_eq!(outcomes.len(), TOTAL);
    assert!(outcomes.iter().all(|o| o.created && !o.idempotent_retry));

    // Event ids match the deterministic derivation.
    for (id, outcome) in object_ids.iter().zip(outcomes.iter()) {
        assert_eq!(
            outcome.event_id,
            derive_event_id(&tenant.0, "object", &id.0, 1)
        );
        assert_eq!(outcome.committed_version, 1);
    }

    // Cassandra holds exactly TOTAL primary rows for this tenant.
    let count_row: (i64,) = cassandra
        .query(
            "SELECT count(*) FROM ontology_objects.objects_by_id WHERE tenant = ? ALLOW FILTERING",
            (tenant.0.as_str(),),
        )
        .await
        .expect("count query")
        .first_row_typed()
        .expect("count row");
    assert_eq!(count_row.0 as usize, TOTAL);

    // Outbox is empty in steady state — INSERT+DELETE collapsed in WAL.
    let outbox_count: (i64,) = sqlx::query_as("SELECT count(*) FROM outbox.events")
        .fetch_one(&pg_pool)
        .await
        .expect("outbox count");
    assert_eq!(outbox_count.0, 0);

    // -- 4. Idempotent retry path --------------------------------------
    // Replay the *same* call for one id; helper must detect the LWT
    // conflict (actual==target) and report `idempotent_retry=true`
    // rather than bubbling the conflict up as an error.
    let id = &object_ids[0];
    let object = Object {
        tenant: tenant.clone(),
        id: id.clone(),
        type_id: TypeId("type_0".to_string()),
        version: 0,
        payload: serde_json::json!({"i": 0}),
        organization_id: None,
        created_at_ms: Some(now_ms),
        updated_at_ms: now_ms,
        owner: Some(owner.clone()),
        markings: vec![MarkingId("PUBLIC".to_string())],
    };
    let retry = apply_object_with_outbox(
        &pg_pool,
        object_store.as_ref(),
        object,
        None,
        "object",
        "ontology.object.changed.v1",
        serde_json::json!({"i": 0}),
    )
    .await
    .expect("retry succeeds via idempotent path");
    assert!(retry.idempotent_retry, "expected idempotent_retry=true");
    assert_eq!(retry.committed_version, 1);
}

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
        .expect("postgres container");

    let host = container.get_host().await.expect("host");
    let port = container
        .get_host_port_ipv4(POSTGRES_PORT)
        .await
        .expect("mapped port");
    let url = format!("postgres://postgres:{PG_PASSWORD}@{host}:{port}/openfoundry");

    let mut attempts = 0;
    let pool = loop {
        match PgPoolOptions::new().max_connections(16).connect(&url).await {
            Ok(p) => break p,
            Err(e) if attempts < 30 => {
                attempts += 1;
                tokio::time::sleep(Duration::from_millis(500)).await;
                eprintln!("waiting for postgres ({attempts}): {e}");
            }
            Err(e) => panic!("postgres unreachable: {e}"),
        }
    };

    sqlx::migrate!("../../libs/outbox/migrations")
        .run(&pool)
        .await
        .expect("apply outbox migrations");

    (container, pool)
}

async fn boot_cassandra() -> (ContainerAsync<GenericImage>, Arc<scylla::Session>) {
    let image = GenericImage::new("cassandra", "5.0.2")
        .with_exposed_port(9042.tcp())
        .with_wait_for(WaitFor::message_on_stdout(
            "Starting listening for CQL clients",
        ));
    let container = image.start().await.expect("cassandra container");
    let host = container.get_host().await.expect("host").to_string();
    let port = container
        .get_host_port_ipv4(9042)
        .await
        .expect("mapped port");

    tokio::time::sleep(Duration::from_secs(5)).await;

    let cfg = ClusterConfig {
        contact_points: vec![format!("{host}:{port}")],
        local_datacenter: "datacenter1".to_string(),
        ..ClusterConfig::dev_local()
    };
    let session = Arc::new(SessionBuilder::new(cfg).build().await.expect("session"));

    session
        .query(
            "CREATE KEYSPACE IF NOT EXISTS ontology_objects WITH replication = \
             {'class': 'SimpleStrategy', 'replication_factor': 1}",
            &[],
        )
        .await
        .expect("create keyspace");

    for cql in [
        "CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_id (
             tenant text, object_id timeuuid, type_id text, owner_id uuid,
             properties text, marking frozen<set<text>>, organization_id uuid,
             revision_number bigint STATIC, created_at timestamp,
             updated_at timestamp, deleted boolean,
             PRIMARY KEY ((tenant, object_id))
         )",
        "CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_type (
             tenant text, type_id text, updated_at timestamp, object_id timeuuid,
             owner_id uuid, marking frozen<set<text>>,
             properties_summary text, deleted boolean,
             PRIMARY KEY ((tenant, type_id), updated_at, object_id)
         ) WITH CLUSTERING ORDER BY (updated_at DESC, object_id ASC)",
        "CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_owner (
             tenant text, owner_id uuid, type_id text, object_id timeuuid,
             updated_at timestamp, deleted boolean,
             PRIMARY KEY ((tenant, owner_id), type_id, object_id)
         )",
        "CREATE TABLE IF NOT EXISTS ontology_objects.objects_by_marking (
             tenant text, marking_id text, object_id timeuuid, type_id text,
             owner_id uuid, updated_at timestamp, deleted boolean,
             PRIMARY KEY ((tenant, marking_id), object_id)
         )",
    ] {
        session.query(cql, &[]).await.expect("create table");
    }

    (container, session)
}
