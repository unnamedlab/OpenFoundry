//! Integration tests for [`CassandraSchemaStore`] and
//! [`CassandraSessionStore`].
//!
//! Boots a real Cassandra container, applies production-shaped
//! `ontology_objects.schemas_*` and `auth_runtime.sessions_by_id` DDL,
//! then validates create/read/latest/update and revoke semantics.
//!
//! Marked `#[ignore]` because CI without Docker would otherwise fail. Run
//! locally with:
//!
//! ```text
//! cargo test -p cassandra-kernel --features repos --test cassandra_schema_session_store_it -- --ignored
//! ```

#![cfg(feature = "repos")]

mod support;

use std::collections::HashMap;

use cassandra_kernel::repos::{CassandraSchemaStore, CassandraSessionStore};
use storage_abstraction::repositories::{
    ReadConsistency, Schema, SchemaStore, Session, SessionStore, TenantId, TypeId,
};

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires docker"]
async fn cassandra_schema_and_session_stores_round_trip() {
    let _ = tracing_subscriber::fmt::try_init();

    let cassandra = support::start_cassandra().await;
    let session = cassandra.session.clone();
    apply_ddl(session.as_ref()).await;

    let schema_store = CassandraSchemaStore::new(session.clone());
    schema_store.warm_up().await.expect("schema warm-up");
    let session_store = CassandraSessionStore::new(session);
    session_store.warm_up().await.expect("session warm-up");

    let type_id = TypeId("aircraft".into());
    assert!(
        schema_store
            .get_latest(&type_id, ReadConsistency::Strong)
            .await
            .expect("get empty latest")
            .is_none()
    );

    let now_ms = chrono::Utc::now().timestamp_millis();
    let v1 = Schema {
        type_id: type_id.clone(),
        version: 1,
        json_schema: serde_json::json!({
            "type": "object",
            "required": ["tail_number"],
            "properties": { "tail_number": { "type": "string" } }
        }),
        created_at_ms: now_ms,
    };
    schema_store.put(v1.clone()).await.expect("put schema v1");
    assert_eq!(
        schema_store
            .get_version(&type_id, 1, ReadConsistency::Strong)
            .await
            .expect("get v1")
            .expect("v1 exists")
            .json_schema,
        v1.json_schema
    );
    assert_eq!(
        schema_store
            .get_latest(&type_id, ReadConsistency::Eventual)
            .await
            .expect("latest v1")
            .expect("latest exists")
            .version,
        1
    );

    let v2 = Schema {
        version: 2,
        json_schema: serde_json::json!({
            "type": "object",
            "required": ["tail_number", "status"],
            "properties": {
                "tail_number": { "type": "string" },
                "status": { "enum": ["active", "retired"] }
            }
        }),
        created_at_ms: now_ms + 1,
        ..v1.clone()
    };
    schema_store.put(v2.clone()).await.expect("put schema v2");
    let latest = schema_store
        .get_latest(&type_id, ReadConsistency::Strong)
        .await
        .expect("latest v2")
        .expect("latest exists");
    assert_eq!(latest.version, 2);
    assert_eq!(latest.json_schema, v2.json_schema);
    assert!(
        schema_store
            .put(v1)
            .await
            .expect_err("stale schema version rejected")
            .to_string()
            .contains("not greater than latest")
    );

    let tenant = TenantId("tenant-a".into());
    let session_id = "session-a";
    let mut attributes = HashMap::new();
    attributes.insert("scope".to_string(), "read:objects".to_string());
    session_store
        .put(Session {
            tenant: tenant.clone(),
            id: session_id.to_string(),
            subject: "user-1".into(),
            attributes,
            issued_at_ms: now_ms,
            expires_at_ms: now_ms + 60_000,
        })
        .await
        .expect("put session");

    let fetched = session_store
        .get(&tenant, session_id, ReadConsistency::Strong)
        .await
        .expect("get session")
        .expect("session exists");
    assert_eq!(fetched.subject, "user-1");
    assert_eq!(
        fetched.attributes.get("scope").map(String::as_str),
        Some("read:objects")
    );

    let mut updated_attributes = HashMap::new();
    updated_attributes.insert("scope".to_string(), "write:objects".to_string());
    session_store
        .put(Session {
            tenant: tenant.clone(),
            id: session_id.to_string(),
            subject: "user-2".into(),
            attributes: updated_attributes,
            issued_at_ms: now_ms,
            expires_at_ms: now_ms + 120_000,
        })
        .await
        .expect("update session");
    let updated = session_store
        .get(&tenant, session_id, ReadConsistency::Eventual)
        .await
        .expect("get updated")
        .expect("session exists");
    assert_eq!(updated.subject, "user-2");
    assert_eq!(
        updated.attributes.get("scope").map(String::as_str),
        Some("write:objects")
    );

    assert!(
        session_store
            .revoke(&tenant, session_id)
            .await
            .expect("revoke existing")
    );
    assert!(
        session_store
            .get(&tenant, session_id, ReadConsistency::Strong)
            .await
            .expect("get revoked")
            .is_none()
    );
    assert!(
        !session_store
            .revoke(&tenant, session_id)
            .await
            .expect("revoke absent")
    );

    session_store
        .put(Session {
            tenant: tenant.clone(),
            id: "expired".into(),
            subject: "user-3".into(),
            attributes: HashMap::new(),
            issued_at_ms: now_ms - 120_000,
            expires_at_ms: now_ms - 60_000,
        })
        .await
        .expect("put expired session is a no-op");
    assert!(
        session_store
            .get(&tenant, "expired", ReadConsistency::Strong)
            .await
            .expect("get expired")
            .is_none()
    );
}

async fn apply_ddl(s: &scylla::Session) {
    s.query(
        "CREATE KEYSPACE IF NOT EXISTS ontology_objects WITH replication = \
         {'class': 'SimpleStrategy', 'replication_factor': 1}",
        &[],
    )
    .await
    .expect("create ontology keyspace");
    s.query(
        "CREATE KEYSPACE IF NOT EXISTS auth_runtime WITH replication = \
         {'class': 'SimpleStrategy', 'replication_factor': 1}",
        &[],
    )
    .await
    .expect("create auth keyspace");

    for cql in [
        "CREATE TABLE IF NOT EXISTS ontology_objects.schemas_by_type (
             type_id text,
             version int,
             json_schema text,
             created_at timestamp,
             PRIMARY KEY ((type_id), version)
         ) WITH CLUSTERING ORDER BY (version DESC)",
        "CREATE TABLE IF NOT EXISTS ontology_objects.schemas_latest (
             type_id text PRIMARY KEY,
             version int,
             json_schema text,
             created_at timestamp
         )",
        "CREATE TABLE IF NOT EXISTS auth_runtime.sessions_by_id (
             tenant text,
             session_id text,
             subject text,
             attributes map<text, text>,
             issued_at timestamp,
             expires_at timestamp,
             PRIMARY KEY ((tenant, session_id))
         )",
    ] {
        s.query(cql, &[]).await.expect("create table");
    }
}
