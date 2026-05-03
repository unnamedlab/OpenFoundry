//! Declarative storage-inspection repository.
//!
//! Runtime object/link/funnel counts are computed through storage traits in the
//! handler. The remaining counts and PG index inventory belong to declarative
//! control-plane metadata, so SQL stays behind this repository boundary.

use serde::Serialize;
use sqlx::PgPool;

use crate::models::{link_type::LinkType, object_type::ObjectType};

pub type StorageRepoResult<T> = Result<T, sqlx::Error>;

#[derive(Debug, Clone, Default)]
pub struct DefinitionCounts {
    pub object_types: i64,
    pub properties: i64,
    pub link_types: i64,
    pub interfaces: i64,
    pub interface_properties: i64,
    pub shared_property_types: i64,
    pub action_types: i64,
    pub function_packages: i64,
    pub object_sets: i64,
    pub projects: i64,
    pub funnel_sources: i64,
}

#[derive(Debug, Clone, Serialize, sqlx::FromRow)]
pub struct StorageIndexDefinition {
    pub table_name: String,
    pub index_name: String,
    pub index_definition: String,
}

pub async fn definition_counts(db: &PgPool) -> StorageRepoResult<DefinitionCounts> {
    Ok(DefinitionCounts {
        object_types: count(db, "object_types").await?,
        properties: count(db, "properties").await?,
        link_types: count(db, "link_types").await?,
        interfaces: count(db, "ontology_interfaces").await?,
        interface_properties: count(db, "interface_properties").await?,
        shared_property_types: count(db, "shared_property_types").await?,
        action_types: count(db, "action_types").await?,
        function_packages: count(db, "ontology_function_packages").await?,
        object_sets: count(db, "ontology_object_sets").await?,
        projects: count(db, "ontology_projects").await?,
        funnel_sources: count(db, "ontology_funnel_sources").await?,
    })
}

pub async fn object_types(db: &PgPool) -> StorageRepoResult<Vec<ObjectType>> {
    sqlx::query_as::<_, ObjectType>("SELECT * FROM object_types ORDER BY created_at DESC")
        .fetch_all(db)
        .await
}

pub async fn link_types(db: &PgPool) -> StorageRepoResult<Vec<LinkType>> {
    sqlx::query_as::<_, LinkType>("SELECT * FROM link_types ORDER BY created_at DESC")
        .fetch_all(db)
        .await
}

async fn count(db: &PgPool, table: &'static str) -> StorageRepoResult<i64> {
    let sql = format!("SELECT COUNT(*) FROM {table}");
    sqlx::query_scalar::<_, i64>(&sql).fetch_one(db).await
}

pub async fn pg_index_definitions(db: &PgPool) -> StorageRepoResult<Vec<StorageIndexDefinition>> {
    sqlx::query_as::<_, StorageIndexDefinition>(
        r#"SELECT
                tablename AS table_name,
                indexname AS index_name,
                indexdef AS index_definition
           FROM pg_indexes
           WHERE schemaname = 'public'
             AND tablename = ANY($1)
           ORDER BY tablename ASC, indexname ASC"#,
    )
    .bind(vec![
        "object_types",
        "properties",
        "link_types",
        "ontology_interfaces",
        "interface_properties",
        "shared_property_types",
        "action_types",
        "ontology_function_packages",
        "ontology_object_sets",
        "ontology_funnel_sources",
        "ontology_projects",
    ])
    .fetch_all(db)
    .await
}

pub fn cassandra_index_definitions() -> Vec<StorageIndexDefinition> {
    vec![
        StorageIndexDefinition {
            table_name: "links_outgoing".to_string(),
            index_name: "links_outgoing_pkey".to_string(),
            index_definition: "PRIMARY KEY ((tenant, source_id), link_type, target_id)".to_string(),
        },
        StorageIndexDefinition {
            table_name: "links_incoming".to_string(),
            index_name: "links_incoming_pkey".to_string(),
            index_definition: "PRIMARY KEY ((tenant, target_id), link_type, source_id)".to_string(),
        },
        StorageIndexDefinition {
            table_name: "actions_log.actions_log".to_string(),
            index_name: "actions_log_pkey".to_string(),
            index_definition: "PRIMARY KEY ((tenant, day_bucket), applied_at, action_id)"
                .to_string(),
        },
        StorageIndexDefinition {
            table_name: "actions_log.actions_by_object".to_string(),
            index_name: "actions_by_object_pkey".to_string(),
            index_definition: "PRIMARY KEY ((tenant, target_object_id), applied_at, action_id)"
                .to_string(),
        },
        StorageIndexDefinition {
            table_name: "actions_log.actions_by_action".to_string(),
            index_name: "actions_by_action_pkey".to_string(),
            index_definition: "PRIMARY KEY ((tenant, action_id, day_bucket), applied_at, event_id)"
                .to_string(),
        },
        StorageIndexDefinition {
            table_name: "actions_log.actions_by_event".to_string(),
            index_name: "actions_by_event_pkey".to_string(),
            index_definition: "PRIMARY KEY ((tenant, event_id))".to_string(),
        },
    ]
}
