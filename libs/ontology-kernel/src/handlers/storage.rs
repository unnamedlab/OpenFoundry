use auth_middleware::{claims::Claims, layer::AuthUser};
use axum::{Json, extract::State, response::IntoResponse};
use chrono::{DateTime, Utc};
use serde::Serialize;
use serde_json::json;
use sqlx::FromRow;
use std::collections::BTreeMap;
use uuid::Uuid;

use crate::{AppState, domain::indexer::build_search_documents};

fn db_error(message: impl Into<String>) -> axum::response::Response {
    (
        axum::http::StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

#[derive(Debug, Clone, Serialize)]
pub struct StorageTableMetric {
    pub key: String,
    pub table_name: String,
    pub label: String,
    pub role: String,
    pub record_count: i64,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct StorageIndexDefinition {
    pub table_name: String,
    pub index_name: String,
    pub index_definition: String,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct StorageDistributionMetric {
    pub id: Uuid,
    pub label: String,
    pub count: i64,
}

#[derive(Debug, Clone, Serialize)]
pub struct StorageSearchKindMetric {
    pub kind: String,
    pub count: i64,
}

#[derive(Debug, Clone, Serialize)]
pub struct OntologyStorageInsightsResponse {
    pub database_backend: String,
    pub access_driver: String,
    pub graph_projection: String,
    pub search_projection: String,
    pub funnel_runtime: String,
    pub table_metrics: Vec<StorageTableMetric>,
    pub index_definitions: Vec<StorageIndexDefinition>,
    pub object_type_distribution: Vec<StorageDistributionMetric>,
    pub link_type_distribution: Vec<StorageDistributionMetric>,
    pub search_documents_total: i64,
    pub search_documents_by_kind: Vec<StorageSearchKindMetric>,
    pub latest_object_write_at: Option<DateTime<Utc>>,
    pub latest_link_write_at: Option<DateTime<Utc>>,
    pub latest_funnel_run_at: Option<DateTime<Utc>>,
}

async fn load_table_metrics(state: &AppState) -> Result<Vec<StorageTableMetric>, sqlx::Error> {
    let object_types = sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM object_types")
        .fetch_one(&state.db)
        .await?;
    let properties = sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM properties")
        .fetch_one(&state.db)
        .await?;
    let link_types = sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM link_types")
        .fetch_one(&state.db)
        .await?;
    let object_instances = sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM object_instances")
        .fetch_one(&state.db)
        .await?;
    let link_instances = sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM link_instances")
        .fetch_one(&state.db)
        .await?;
    let interfaces = sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM ontology_interfaces")
        .fetch_one(&state.db)
        .await?;
    let interface_properties =
        sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM interface_properties")
            .fetch_one(&state.db)
            .await?;
    let shared_property_types =
        sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM shared_property_types")
            .fetch_one(&state.db)
            .await?;
    let action_types = sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM action_types")
        .fetch_one(&state.db)
        .await?;
    let function_packages =
        sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM ontology_function_packages")
            .fetch_one(&state.db)
            .await?;
    let object_sets = sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM ontology_object_sets")
        .fetch_one(&state.db)
        .await?;
    let projects = sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM ontology_projects")
        .fetch_one(&state.db)
        .await?;
    let funnel_sources =
        sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM ontology_funnel_sources")
            .fetch_one(&state.db)
            .await?;
    let funnel_runs = sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM ontology_funnel_runs")
        .fetch_one(&state.db)
        .await?;

    Ok(vec![
        StorageTableMetric {
            key: "object_types".to_string(),
            table_name: "object_types".to_string(),
            label: "Object types".to_string(),
            role: "Schema".to_string(),
            record_count: object_types,
        },
        StorageTableMetric {
            key: "properties".to_string(),
            table_name: "properties".to_string(),
            label: "Properties".to_string(),
            role: "Schema".to_string(),
            record_count: properties,
        },
        StorageTableMetric {
            key: "link_types".to_string(),
            table_name: "link_types".to_string(),
            label: "Link types".to_string(),
            role: "Schema".to_string(),
            record_count: link_types,
        },
        StorageTableMetric {
            key: "interfaces".to_string(),
            table_name: "ontology_interfaces".to_string(),
            label: "Interfaces".to_string(),
            role: "Schema".to_string(),
            record_count: interfaces,
        },
        StorageTableMetric {
            key: "interface_properties".to_string(),
            table_name: "interface_properties".to_string(),
            label: "Interface properties".to_string(),
            role: "Schema".to_string(),
            record_count: interface_properties,
        },
        StorageTableMetric {
            key: "shared_property_types".to_string(),
            table_name: "shared_property_types".to_string(),
            label: "Shared property types".to_string(),
            role: "Schema".to_string(),
            record_count: shared_property_types,
        },
        StorageTableMetric {
            key: "action_types".to_string(),
            table_name: "action_types".to_string(),
            label: "Action types".to_string(),
            role: "Runtime".to_string(),
            record_count: action_types,
        },
        StorageTableMetric {
            key: "function_packages".to_string(),
            table_name: "ontology_function_packages".to_string(),
            label: "Function packages".to_string(),
            role: "Runtime".to_string(),
            record_count: function_packages,
        },
        StorageTableMetric {
            key: "object_instances".to_string(),
            table_name: "object_instances".to_string(),
            label: "Object rows".to_string(),
            role: "Runtime".to_string(),
            record_count: object_instances,
        },
        StorageTableMetric {
            key: "link_instances".to_string(),
            table_name: "link_instances".to_string(),
            label: "Link rows".to_string(),
            role: "Runtime".to_string(),
            record_count: link_instances,
        },
        StorageTableMetric {
            key: "object_sets".to_string(),
            table_name: "ontology_object_sets".to_string(),
            label: "Object sets".to_string(),
            role: "Runtime".to_string(),
            record_count: object_sets,
        },
        StorageTableMetric {
            key: "funnel_sources".to_string(),
            table_name: "ontology_funnel_sources".to_string(),
            label: "Funnel sources".to_string(),
            role: "Ingestion".to_string(),
            record_count: funnel_sources,
        },
        StorageTableMetric {
            key: "funnel_runs".to_string(),
            table_name: "ontology_funnel_runs".to_string(),
            label: "Funnel runs".to_string(),
            role: "Ingestion".to_string(),
            record_count: funnel_runs,
        },
        StorageTableMetric {
            key: "projects".to_string(),
            table_name: "ontology_projects".to_string(),
            label: "Ontology projects".to_string(),
            role: "Governance".to_string(),
            record_count: projects,
        },
    ])
}

async fn load_index_definitions(
    state: &AppState,
) -> Result<Vec<StorageIndexDefinition>, sqlx::Error> {
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
        "object_instances",
        "link_instances",
        "ontology_interfaces",
        "interface_properties",
        "shared_property_types",
        "action_types",
        "ontology_function_packages",
        "ontology_object_sets",
        "ontology_funnel_sources",
        "ontology_funnel_runs",
        "ontology_projects",
    ])
    .fetch_all(&state.db)
    .await
}

async fn load_object_type_distribution(
    state: &AppState,
) -> Result<Vec<StorageDistributionMetric>, sqlx::Error> {
    sqlx::query_as::<_, StorageDistributionMetric>(
        r#"SELECT
                ot.id,
                ot.display_name AS label,
                COUNT(oi.id)::BIGINT AS count
           FROM object_types ot
           LEFT JOIN object_instances oi ON oi.object_type_id = ot.id
           GROUP BY ot.id, ot.display_name
           ORDER BY count DESC, ot.display_name ASC
           LIMIT 8"#,
    )
    .fetch_all(&state.db)
    .await
}

async fn load_link_type_distribution(
    state: &AppState,
) -> Result<Vec<StorageDistributionMetric>, sqlx::Error> {
    sqlx::query_as::<_, StorageDistributionMetric>(
        r#"SELECT
                lt.id,
                lt.display_name AS label,
                COUNT(li.id)::BIGINT AS count
           FROM link_types lt
           LEFT JOIN link_instances li ON li.link_type_id = lt.id
           GROUP BY lt.id, lt.display_name
           ORDER BY count DESC, lt.display_name ASC
           LIMIT 8"#,
    )
    .fetch_all(&state.db)
    .await
}

async fn load_search_document_metrics(
    state: &AppState,
    claims: &Claims,
) -> Result<(i64, Vec<StorageSearchKindMetric>), sqlx::Error> {
    let documents = build_search_documents(state, claims, None, None).await?;
    let mut by_kind = BTreeMap::<String, i64>::new();
    for document in &documents {
        *by_kind.entry(document.kind.clone()).or_default() += 1;
    }
    let mut kinds = by_kind
        .into_iter()
        .map(|(kind, count)| StorageSearchKindMetric { kind, count })
        .collect::<Vec<_>>();
    kinds.sort_by(|left, right| {
        right
            .count
            .cmp(&left.count)
            .then_with(|| left.kind.cmp(&right.kind))
    });
    Ok((documents.len() as i64, kinds))
}

pub async fn get_storage_insights(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
) -> impl IntoResponse {
    let table_metrics = match load_table_metrics(&state).await {
        Ok(metrics) => metrics,
        Err(error) => return db_error(format!("failed to load storage table metrics: {error}")),
    };

    let index_definitions = match load_index_definitions(&state).await {
        Ok(definitions) => definitions,
        Err(error) => {
            return db_error(format!("failed to load storage index definitions: {error}"));
        }
    };

    let object_type_distribution = match load_object_type_distribution(&state).await {
        Ok(distribution) => distribution,
        Err(error) => return db_error(format!("failed to load object distributions: {error}")),
    };

    let link_type_distribution = match load_link_type_distribution(&state).await {
        Ok(distribution) => distribution,
        Err(error) => return db_error(format!("failed to load link distributions: {error}")),
    };

    let (search_documents_total, search_documents_by_kind) =
        match load_search_document_metrics(&state, &claims).await {
            Ok(metrics) => metrics,
            Err(error) => {
                return db_error(format!("failed to load search document metrics: {error}"));
            }
        };

    let latest_object_write_at = match sqlx::query_scalar::<_, Option<DateTime<Utc>>>(
        "SELECT MAX(updated_at) FROM object_instances",
    )
    .fetch_one(&state.db)
    .await
    {
        Ok(value) => value,
        Err(error) => return db_error(format!("failed to load latest object write: {error}")),
    };

    let latest_link_write_at = match sqlx::query_scalar::<_, Option<DateTime<Utc>>>(
        "SELECT MAX(created_at) FROM link_instances",
    )
    .fetch_one(&state.db)
    .await
    {
        Ok(value) => value,
        Err(error) => return db_error(format!("failed to load latest link write: {error}")),
    };

    let latest_funnel_run_at = match sqlx::query_scalar::<_, Option<DateTime<Utc>>>(
        "SELECT MAX(started_at) FROM ontology_funnel_runs",
    )
    .fetch_one(&state.db)
    .await
    {
        Ok(value) => value,
        Err(error) => return db_error(format!("failed to load latest funnel run: {error}")),
    };

    Json(json!(OntologyStorageInsightsResponse {
        database_backend: "PostgreSQL".to_string(),
        access_driver: "sqlx".to_string(),
        graph_projection: "link_types + link_instances".to_string(),
        search_projection: "domain::indexer search documents".to_string(),
        funnel_runtime: "ontology_funnel_sources + ontology_funnel_runs".to_string(),
        table_metrics,
        index_definitions,
        object_type_distribution,
        link_type_distribution,
        search_documents_total,
        search_documents_by_kind,
        latest_object_write_at,
        latest_link_write_at,
        latest_funnel_run_at,
    }))
    .into_response()
}
