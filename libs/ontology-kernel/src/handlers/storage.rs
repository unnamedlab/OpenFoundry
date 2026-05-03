use auth_middleware::{claims::Claims, layer::AuthUser};
use axum::{Json, extract::State, response::IntoResponse};
use chrono::{DateTime, Utc};
use serde::Serialize;
use serde_json::json;
use std::collections::BTreeMap;
use storage_abstraction::repositories::{Page, ReadConsistency, TypeId};
use uuid::Uuid;

use crate::{
    AppState,
    domain::{
        funnel_repository, indexer::build_search_documents, read_models::tenant_from_claims,
        storage_repository,
    },
    handlers::links::collect_link_instances_for_type,
    models::{link_type::LinkType, object_type::ObjectType},
};

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

pub use crate::domain::storage_repository::StorageIndexDefinition;

#[derive(Debug, Clone, Serialize)]
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

struct LinkRuntimeMetrics {
    total: i64,
    distribution: Vec<StorageDistributionMetric>,
    latest_write_at: Option<DateTime<Utc>>,
}

struct ObjectRuntimeMetrics {
    total: i64,
    distribution: Vec<StorageDistributionMetric>,
    latest_write_at: Option<DateTime<Utc>>,
}

struct FunnelRuntimeMetrics {
    total: i64,
    latest_run_at: Option<DateTime<Utc>>,
}

fn utc_from_millis(ms: i64) -> Option<DateTime<Utc>> {
    chrono::TimeZone::timestamp_millis_opt(&Utc, ms).single()
}

async fn load_object_types(
    state: &AppState,
) -> storage_repository::StorageRepoResult<Vec<ObjectType>> {
    storage_repository::object_types(&state.db).await
}

async fn load_object_runtime_metrics(
    state: &AppState,
    claims: &Claims,
) -> Result<ObjectRuntimeMetrics, String> {
    let tenant = tenant_from_claims(claims);
    let object_types = load_object_types(state)
        .await
        .map_err(|error| format!("failed to load object type metadata: {error}"))?;
    let mut total = 0i64;
    let mut latest_write_at = None;
    let mut distribution = Vec::with_capacity(object_types.len());

    for object_type in object_types {
        let mut count = 0i64;
        let mut token = None;
        loop {
            let page = state
                .stores
                .objects
                .list_by_type(
                    &tenant,
                    &TypeId(object_type.id.to_string()),
                    Page {
                        size: 200,
                        token: token.clone(),
                    },
                    ReadConsistency::Strong,
                )
                .await
                .map_err(|error| {
                    format!(
                        "failed to list object runtime metrics for type {}: {error}",
                        object_type.id
                    )
                })?;

            for object in page.items {
                count += 1;
                latest_write_at = latest_write_at.max(utc_from_millis(object.updated_at_ms));
            }

            match page.next_token {
                Some(next) => token = Some(next),
                None => break,
            }
        }

        total += count;
        distribution.push(StorageDistributionMetric {
            id: object_type.id,
            label: object_type.display_name,
            count,
        });
    }

    distribution.sort_by(|left, right| {
        right
            .count
            .cmp(&left.count)
            .then_with(|| left.label.cmp(&right.label))
    });
    distribution.truncate(8);

    Ok(ObjectRuntimeMetrics {
        total,
        distribution,
        latest_write_at,
    })
}

async fn load_link_types(state: &AppState) -> storage_repository::StorageRepoResult<Vec<LinkType>> {
    storage_repository::link_types(&state.db).await
}

async fn load_link_runtime_metrics(
    state: &AppState,
    claims: &Claims,
) -> Result<LinkRuntimeMetrics, String> {
    let tenant = tenant_from_claims(claims);
    let link_types = load_link_types(state)
        .await
        .map_err(|error| format!("failed to load link type metadata: {error}"))?;
    let mut total = 0i64;
    let mut latest_write_at = None;
    let mut distribution = Vec::with_capacity(link_types.len());

    for link_type in link_types {
        let links = collect_link_instances_for_type(state, &tenant, &link_type).await?;
        let count = links.len() as i64;
        total += count;
        latest_write_at = latest_write_at.max(links.iter().map(|link| link.created_at).max());
        distribution.push(StorageDistributionMetric {
            id: link_type.id,
            label: link_type.display_name,
            count,
        });
    }

    distribution.sort_by(|left, right| {
        right
            .count
            .cmp(&left.count)
            .then_with(|| left.label.cmp(&right.label))
    });
    distribution.truncate(8);

    Ok(LinkRuntimeMetrics {
        total,
        distribution,
        latest_write_at,
    })
}

async fn load_funnel_runtime_metrics(
    state: &AppState,
    claims: &Claims,
) -> Result<FunnelRuntimeMetrics, String> {
    let tenant = tenant_from_claims(claims);
    let runs = funnel_repository::list_runs_for_tenant(state.stores.actions.as_ref(), &tenant)
        .await
        .map_err(|error| format!("failed to list funnel runtime metrics: {error}"))?;
    let latest_run_at = runs.iter().map(|run| run.started_at).max();

    Ok(FunnelRuntimeMetrics {
        total: runs.len() as i64,
        latest_run_at,
    })
}

async fn load_table_metrics(
    state: &AppState,
    object_runtime_total: i64,
    link_runtime_total: i64,
    funnel_runtime_total: i64,
) -> storage_repository::StorageRepoResult<Vec<StorageTableMetric>> {
    let counts = storage_repository::definition_counts(&state.db).await?;

    Ok(vec![
        StorageTableMetric {
            key: "object_types".to_string(),
            table_name: "object_types".to_string(),
            label: "Object types".to_string(),
            role: "Schema".to_string(),
            record_count: counts.object_types,
        },
        StorageTableMetric {
            key: "properties".to_string(),
            table_name: "properties".to_string(),
            label: "Properties".to_string(),
            role: "Schema".to_string(),
            record_count: counts.properties,
        },
        StorageTableMetric {
            key: "link_types".to_string(),
            table_name: "link_types".to_string(),
            label: "Link types".to_string(),
            role: "Schema".to_string(),
            record_count: counts.link_types,
        },
        StorageTableMetric {
            key: "interfaces".to_string(),
            table_name: "ontology_interfaces".to_string(),
            label: "Interfaces".to_string(),
            role: "Schema".to_string(),
            record_count: counts.interfaces,
        },
        StorageTableMetric {
            key: "interface_properties".to_string(),
            table_name: "interface_properties".to_string(),
            label: "Interface properties".to_string(),
            role: "Schema".to_string(),
            record_count: counts.interface_properties,
        },
        StorageTableMetric {
            key: "shared_property_types".to_string(),
            table_name: "shared_property_types".to_string(),
            label: "Shared property types".to_string(),
            role: "Schema".to_string(),
            record_count: counts.shared_property_types,
        },
        StorageTableMetric {
            key: "action_types".to_string(),
            table_name: "action_types".to_string(),
            label: "Action types".to_string(),
            role: "Runtime".to_string(),
            record_count: counts.action_types,
        },
        StorageTableMetric {
            key: "function_packages".to_string(),
            table_name: "ontology_function_packages".to_string(),
            label: "Function packages".to_string(),
            role: "Runtime".to_string(),
            record_count: counts.function_packages,
        },
        StorageTableMetric {
            key: "object_instances".to_string(),
            table_name: "ontology_objects.objects_by_id".to_string(),
            label: "Object rows".to_string(),
            role: "Runtime".to_string(),
            record_count: object_runtime_total,
        },
        StorageTableMetric {
            key: "link_instances".to_string(),
            table_name: "links_outgoing + links_incoming".to_string(),
            label: "Link rows".to_string(),
            role: "Runtime".to_string(),
            record_count: link_runtime_total,
        },
        StorageTableMetric {
            key: "object_sets".to_string(),
            table_name: "ontology_object_sets".to_string(),
            label: "Object sets".to_string(),
            role: "Runtime".to_string(),
            record_count: counts.object_sets,
        },
        StorageTableMetric {
            key: "funnel_sources".to_string(),
            table_name: "ontology_funnel_sources".to_string(),
            label: "Funnel sources".to_string(),
            role: "Ingestion".to_string(),
            record_count: counts.funnel_sources,
        },
        StorageTableMetric {
            key: "funnel_runs".to_string(),
            table_name: "actions_log.actions_log(kind=funnel_run)".to_string(),
            label: "Funnel runs".to_string(),
            role: "Ingestion".to_string(),
            record_count: funnel_runtime_total,
        },
        StorageTableMetric {
            key: "projects".to_string(),
            table_name: "ontology_projects".to_string(),
            label: "Ontology projects".to_string(),
            role: "Governance".to_string(),
            record_count: counts.projects,
        },
    ])
}

async fn load_index_definitions(
    state: &AppState,
) -> storage_repository::StorageRepoResult<Vec<StorageIndexDefinition>> {
    let mut definitions = storage_repository::pg_index_definitions(&state.db).await?;
    definitions.extend(storage_repository::cassandra_index_definitions());
    definitions.sort_by(|left, right| {
        left.table_name
            .cmp(&right.table_name)
            .then_with(|| left.index_name.cmp(&right.index_name))
    });

    Ok(definitions)
}

async fn load_search_document_metrics(
    state: &AppState,
    claims: &Claims,
) -> Result<(i64, Vec<StorageSearchKindMetric>), String> {
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
    let object_runtime_metrics = match load_object_runtime_metrics(&state, &claims).await {
        Ok(metrics) => metrics,
        Err(error) => return db_error(format!("failed to load object runtime metrics: {error}")),
    };

    let link_runtime_metrics = match load_link_runtime_metrics(&state, &claims).await {
        Ok(metrics) => metrics,
        Err(error) => return db_error(format!("failed to load link runtime metrics: {error}")),
    };

    let funnel_runtime_metrics = match load_funnel_runtime_metrics(&state, &claims).await {
        Ok(metrics) => metrics,
        Err(error) => return db_error(format!("failed to load funnel runtime metrics: {error}")),
    };

    let table_metrics = match load_table_metrics(
        &state,
        object_runtime_metrics.total,
        link_runtime_metrics.total,
        funnel_runtime_metrics.total,
    )
    .await
    {
        Ok(metrics) => metrics,
        Err(error) => return db_error(format!("failed to load storage table metrics: {error}")),
    };

    let index_definitions = match load_index_definitions(&state).await {
        Ok(definitions) => definitions,
        Err(error) => {
            return db_error(format!("failed to load storage index definitions: {error}"));
        }
    };

    let object_type_distribution = object_runtime_metrics.distribution.clone();

    let (search_documents_total, search_documents_by_kind) =
        match load_search_document_metrics(&state, &claims).await {
            Ok(metrics) => metrics,
            Err(error) => {
                return db_error(format!("failed to load search document metrics: {error}"));
            }
        };

    let latest_object_write_at = object_runtime_metrics.latest_write_at;

    Json(json!(OntologyStorageInsightsResponse {
        database_backend: "PostgreSQL + Cassandra".to_string(),
        access_driver: "repository + storage-abstraction".to_string(),
        graph_projection: "link_types + LinkStore/Cassandra".to_string(),
        search_projection: "domain::indexer search documents".to_string(),
        funnel_runtime: "ontology_funnel_sources + actions_log.actions_log".to_string(),
        table_metrics,
        index_definitions,
        object_type_distribution,
        link_type_distribution: link_runtime_metrics.distribution,
        search_documents_total,
        search_documents_by_kind,
        latest_object_write_at,
        latest_link_write_at: link_runtime_metrics.latest_write_at,
        latest_funnel_run_at: funnel_runtime_metrics.latest_run_at,
    }))
    .into_response()
}
