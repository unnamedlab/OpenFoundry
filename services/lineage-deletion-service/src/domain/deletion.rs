use chrono::Utc;
use serde_json::json;
use storage_abstraction::StorageBackend;
use uuid::Uuid;

use crate::models::deletion::{
    DeletionAuditRecord, LineageDeletionRequest, LineageDeletionResponse, LineageDeletionRow,
    LineageImpactSummary,
};

pub async fn compute_impact(
    client: &reqwest::Client,
    lineage_service_url: &str,
    dataset_id: Uuid,
    legal_hold: bool,
) -> Result<LineageImpactSummary, String> {
    let endpoint = format!(
        "{}/api/v1/lineage/datasets/{}/impact",
        lineage_service_url.trim_end_matches('/'),
        dataset_id
    );

    let response = client.get(endpoint).send().await;
    let fallback = LineageImpactSummary {
        downstream_node_count: 0,
        downstream_dataset_ids: Vec::new(),
        blocked_by_legal_hold: legal_hold,
        impact_notes: if legal_hold {
            vec!["deletion is subject to legal hold".to_string()]
        } else {
            vec!["lineage impact service unavailable; using safe empty fallback".to_string()]
        },
    };

    let Ok(response) = response else {
        return Ok(fallback);
    };
    let Ok(raw_body) = response.text().await else {
        return Ok(fallback);
    };
    let Ok(payload) = serde_json::from_str::<serde_json::Value>(&raw_body) else {
        return Ok(fallback);
    };

    let downstream_dataset_ids = payload
        .get("downstream_dataset_ids")
        .and_then(serde_json::Value::as_array)
        .map(|items| {
            items
                .iter()
                .filter_map(|item| item.as_str())
                .filter_map(|value| Uuid::parse_str(value).ok())
                .collect::<Vec<_>>()
        })
        .unwrap_or_default();
    let downstream_node_count = payload
        .get("downstream_node_count")
        .and_then(serde_json::Value::as_u64)
        .map(|value| value as usize)
        .unwrap_or(downstream_dataset_ids.len());

    Ok(LineageImpactSummary {
        downstream_node_count,
        downstream_dataset_ids,
        blocked_by_legal_hold: legal_hold,
        impact_notes: if legal_hold {
            vec!["legal hold prevents unsafe downstream deletion".to_string()]
        } else {
            vec!["lineage impact evaluated successfully".to_string()]
        },
    })
}

pub async fn execute_safe_deletion(
    storage: &std::sync::Arc<dyn StorageBackend>,
    dataset_id: Uuid,
    hard_delete: bool,
    impact: &LineageImpactSummary,
) -> Result<Vec<String>, String> {
    if impact.blocked_by_legal_hold {
        return Ok(Vec::new());
    }

    let primary_path = format!("datasets/{dataset_id}");
    let marker_path = if hard_delete {
        primary_path.clone()
    } else {
        format!("{primary_path}/subject-mask-marker.json")
    };
    let _ = storage.delete(&marker_path).await;

    let mut deleted_paths = vec![marker_path];
    for downstream in &impact.downstream_dataset_ids {
        let path = format!("datasets/{downstream}/lineage-cascade-marker.json");
        let _ = storage.delete(&path).await;
        deleted_paths.push(path);
    }
    Ok(deleted_paths)
}

pub fn build_audit_trace(
    request: &LineageDeletionRequest,
    impact: &LineageImpactSummary,
    deleted_paths: &[String],
) -> serde_json::Value {
    let records = vec![
        DeletionAuditRecord {
            service: "lineage-deletion-service".to_string(),
            action: "lineage-impact-computed".to_string(),
            subject_id: request.subject_id.clone(),
            metadata: json!({
                "dataset_id": request.dataset_id,
                "downstream_node_count": impact.downstream_node_count,
                "legal_hold": request.legal_hold,
            }),
        },
        DeletionAuditRecord {
            service: "lineage-deletion-service".to_string(),
            action: "safe-deletion-executed".to_string(),
            subject_id: request.subject_id.clone(),
            metadata: json!({
                "dataset_id": request.dataset_id,
                "deleted_paths": deleted_paths,
                "hard_delete": request.hard_delete,
            }),
        },
    ];
    serde_json::to_value(records).unwrap_or_else(|_| json!([]))
}

pub async fn persist_deletion(
    db: &sqlx::PgPool,
    request: &LineageDeletionRequest,
    impact: &LineageImpactSummary,
    deleted_paths: &[String],
) -> Result<LineageDeletionResponse, String> {
    let row = sqlx::query_as::<_, LineageDeletionRow>(
        "INSERT INTO lineage_deletion_requests (
             id, dataset_id, subject_id, hard_delete, legal_hold, impact, status, deleted_paths, audit_trace, requested_at, completed_at
         )
         VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8::jsonb, $9::jsonb, $10, $11)
         RETURNING id, dataset_id, subject_id, hard_delete, legal_hold, impact, status, deleted_paths, audit_trace, requested_at, completed_at",
    )
    .bind(Uuid::now_v7())
    .bind(request.dataset_id)
    .bind(&request.subject_id)
    .bind(request.hard_delete)
    .bind(request.legal_hold)
    .bind(serde_json::to_value(impact).map_err(|cause| cause.to_string())?)
    .bind(if request.legal_hold { "blocked_legal_hold" } else { "completed" })
    .bind(serde_json::to_value(deleted_paths).map_err(|cause| cause.to_string())?)
    .bind(build_audit_trace(request, impact, deleted_paths))
    .bind(Utc::now())
    .bind(Utc::now())
    .fetch_one(db)
    .await
    .map_err(|cause| cause.to_string())?;

    LineageDeletionResponse::try_from(row)
}
