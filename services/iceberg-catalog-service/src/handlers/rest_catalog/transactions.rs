//! `POST /iceberg/v1/transactions/commit` — multi-table commit.
//!
//! P2 upgrade. Implements all-or-nothing semantics across an arbitrary
//! number of tables in a single Postgres transaction:
//!
//!   1. `BEGIN` a database transaction.
//!   2. For each table in the body:
//!        a. `SELECT … FROM iceberg_tables WHERE id = $1 FOR UPDATE`
//!           — row-level lock so no other commit / compaction can
//!           interleave with this one.
//!        b. Validate every `requirement` (assert-uuid,
//!           assert-current-schema-id, assert-ref-snapshot-id, …).
//!        c. Run schema-strict-mode against any `add-schema` update
//!           that snuck in through the multi-table path. Reject with
//!           422 if the schema diff is non-empty.
//!        d. Apply updates, generate the new metadata version, persist
//!           the metadata-file row.
//!   3. `COMMIT` if every table check passed; `ROLLBACK` otherwise.
//!
//! When a row lock cannot be acquired (because another commit holds
//! it), the catalog surfaces the conflict as `Retryable` so the
//! pipeline-build executor can re-snapshot and retry the job.

use axum::extract::State;
use axum::http::HeaderMap;
use axum::{Json, http::StatusCode};
use chrono::Utc;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::Row;
use uuid::Uuid;

use crate::AppState;
use crate::audit;
use crate::domain::branch_alias;
use crate::domain::foundry_transaction::ConflictKind;
use crate::domain::metadata;
use crate::domain::namespace::{self, decode_path};
use crate::domain::schema_strict;
use crate::domain::snapshot;
use crate::domain::table;
use crate::handlers::auth::bearer::AuthenticatedPrincipal;
use crate::handlers::errors::ApiError;
use crate::handlers::rest_catalog::resolve_project_rid;
use crate::metrics;

#[derive(Debug, Deserialize)]
pub struct MultiTableCommitRequest {
    #[serde(rename = "table-changes")]
    pub table_changes: Vec<TableChange>,
    /// Optional `build_rid` so the catalog can correlate the commit
    /// with the originating Foundry build (audit trail + metric
    /// labels).
    #[serde(default)]
    pub build_rid: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct TableChange {
    pub identifier: TableIdentifier,
    #[serde(default)]
    pub requirements: Vec<Value>,
    #[serde(default)]
    pub updates: Vec<Value>,
}

#[derive(Debug, Deserialize, Serialize)]
pub struct TableIdentifier {
    pub namespace: Vec<String>,
    pub name: String,
}

#[derive(Debug, Serialize)]
pub struct MultiTableCommitResponse {
    pub committed: Vec<CommittedTable>,
}

#[derive(Debug, Serialize)]
pub struct CommittedTable {
    pub identifier: TableIdentifier,
    pub table_rid: String,
    pub new_snapshot_id: Option<i64>,
    #[serde(rename = "metadata-location")]
    pub metadata_location: String,
}

pub async fn multi_table_commit(
    State(state): State<AppState>,
    headers: HeaderMap,
    principal: AuthenticatedPrincipal,
    Json(body): Json<MultiTableCommitRequest>,
) -> Result<Json<MultiTableCommitResponse>, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let actor = parse_actor(&principal);
    let build_rid = body.build_rid.clone().unwrap_or_default();

    if !build_rid.is_empty() {
        audit::transaction_begin(actor, &build_rid);
    }

    // ── Phase 1 ─ resolve namespaces + tables outside the lock ────────
    // We collect (table.id, change) pairs so we can take FOR UPDATE
    // locks in id-sort order (deadlock-free).
    let mut resolved: Vec<(table::IcebergTable, &TableChange)> = Vec::new();
    for (idx, change) in body.table_changes.iter().enumerate() {
        if change.identifier.namespace.is_empty() {
            return Err(ApiError::BadRequest(format!(
                "table-changes[{idx}] missing namespace"
            )));
        }
        let ns =
            namespace::fetch(&state.iceberg.db, &project_rid, &change.identifier.namespace).await?;
        let tab = table::fetch(&state.iceberg.db, &ns, &change.identifier.name).await?;
        resolved.push((tab, change));
    }
    resolved.sort_by_key(|(tab, _)| tab.id);

    // ── Phase 2 ─ atomic commit ───────────────────────────────────────
    let mut tx = state
        .iceberg
        .db
        .begin()
        .await
        .map_err(|err| ApiError::Internal(format!("begin tx: {err}")))?;

    let mut committed: Vec<CommittedTable> = Vec::new();

    for (tab, change) in resolved.iter() {
        // 2a — row-level lock. Postgres blocks here until any other
        // commit holding the row releases it; once we have it, no
        // concurrent updater can interleave.
        let lock_row = sqlx::query(
            r#"
            SELECT current_snapshot_id, current_metadata_location, last_sequence_number,
                   schema_json
            FROM iceberg_tables
            WHERE id = $1
            FOR UPDATE
            "#,
        )
        .bind(tab.id)
        .fetch_one(&mut *tx)
        .await
        .map_err(|err| {
            metrics::COMMIT_CONFLICTS_TOTAL
                .with_label_values(&["unknown"])
                .inc();
            audit::transaction_conflict(actor, &build_rid, &tab.rid, "unknown");
            ApiError::Retryable {
                table_rid: tab.rid.clone(),
                reason: format!("unable to acquire row lock: {err}"),
                conflicting_with: ConflictKind::Unknown.as_str().to_string(),
            }
        })?;

        let current_snapshot: Option<i64> = lock_row
            .try_get("current_snapshot_id")
            .map_err(|err| ApiError::Internal(err.to_string()))?;
        let schema_json: Value = lock_row
            .try_get("schema_json")
            .map_err(|err| ApiError::Internal(err.to_string()))?;

        // 2b — requirements check. Mirrors `table::apply_commit` but
        // runs against the **locked** snapshot id so it sees the latest
        // committed view of the row.
        for req in change.requirements.iter() {
            let kind = req.get("type").and_then(Value::as_str).unwrap_or_default();
            match kind {
                "assert-uuid" => {
                    let expected = req.get("uuid").and_then(Value::as_str).unwrap_or_default();
                    if expected != tab.table_uuid {
                        return Err(rollback_with(
                            tx,
                            &build_rid,
                            actor,
                            &tab.rid,
                            ConflictKind::UserJob,
                            format!("assert-uuid: expected {expected}, found {}", tab.table_uuid),
                        )
                        .await);
                    }
                }
                "assert-current-schema-id" => {
                    let expected = req
                        .get("current-schema-id")
                        .and_then(Value::as_i64)
                        .unwrap_or_default();
                    let observed = schema_json
                        .get("schema-id")
                        .and_then(Value::as_i64)
                        .unwrap_or(0);
                    if expected != observed {
                        return Err(rollback_with(
                            tx,
                            &build_rid,
                            actor,
                            &tab.rid,
                            ConflictKind::Compaction,
                            format!(
                                "assert-current-schema-id: expected {expected}, found {observed}"
                            ),
                        )
                        .await);
                    }
                }
                "assert-ref-snapshot-id" => {
                    // Apply Foundry's master↔main alias so a build that
                    // points at `master` lands on Iceberg's `main`.
                    let raw_ref = req.get("ref").and_then(Value::as_str).unwrap_or("main");
                    let resolved = branch_alias::resolve_branch_alias_outcome(raw_ref);
                    if resolved.aliased {
                        metrics::BRANCH_ALIAS_APPLIED_TOTAL
                            .with_label_values(&[&resolved.input, &resolved.resolved])
                            .inc();
                        audit::branch_alias_applied(Some(actor), &resolved.input, &resolved.resolved);
                    }
                    let expected = req.get("snapshot-id").and_then(Value::as_i64);
                    if expected != current_snapshot {
                        return Err(rollback_with(
                            tx,
                            &build_rid,
                            actor,
                            &tab.rid,
                            ConflictKind::Compaction,
                            format!(
                                "assert-ref-snapshot-id: expected {expected:?}, found {current_snapshot:?}"
                            ),
                        )
                        .await);
                    }
                }
                _ => {}
            }
        }

        // 2c — schema strict-mode. The multi-table path is the one a
        // build executor uses; we never let an implicit schema change
        // sneak through here.
        for update in change.updates.iter() {
            let action = update.get("action").and_then(Value::as_str).unwrap_or_default();
            if action == "add-schema" {
                if let Some(attempted) = update.get("schema") {
                    let diff = schema_strict::diff_schemas(&schema_json, attempted);
                    if !diff.is_compatible() {
                        for delta in diff.deltas.iter() {
                            metrics::SCHEMA_STRICT_REJECTIONS_TOTAL
                                .with_label_values(&[delta_kind_label(delta)])
                                .inc();
                        }
                        audit::schema_attempt_blocked(actor, &tab.rid, &diff.rendered());
                        let _ = tx.rollback().await;
                        return Err(ApiError::SchemaIncompatible {
                            current_schema: schema_json.clone(),
                            attempted_schema: attempted.clone(),
                            diff,
                        });
                    }
                }
            }
        }

        // 2d — apply updates. We compose the updates in-memory and
        // then issue a single UPDATE inside the same tx so the row
        // moves atomically.
        let next_state = apply_updates_in_tx(&mut tx, tab.id, &schema_json, &change.updates)
            .await
            .map_err(|err| ApiError::Internal(err.to_string()))?;

        // 2e — record the new metadata version.
        let next_version = next_metadata_version_in_tx(&mut tx, tab.id).await? + 1;
        let metadata_location = format!(
            "{}/metadata/v{}.metadata.json",
            tab.location, next_version
        );
        sqlx::query(
            r#"
            INSERT INTO iceberg_table_metadata_files (id, table_id, version, path)
            VALUES ($1, $2, $3, $4)
            "#,
        )
        .bind(Uuid::now_v7())
        .bind(tab.id)
        .bind(next_version as i32)
        .bind(&metadata_location)
        .execute(&mut *tx)
        .await
        .map_err(|err| ApiError::Internal(err.to_string()))?;

        let _snapshot_id_after = current_snapshot.unwrap_or(0);
        sqlx::query(
            r#"
            UPDATE iceberg_tables
            SET current_metadata_location = $2,
                last_sequence_number = $3,
                updated_at = NOW()
            WHERE id = $1
            "#,
        )
        .bind(tab.id)
        .bind(&metadata_location)
        .bind(tab.last_sequence_number + 1)
        .execute(&mut *tx)
        .await
        .map_err(|err| ApiError::Internal(err.to_string()))?;

        committed.push(CommittedTable {
            identifier: TableIdentifier {
                namespace: decode_path(&tab.namespace_path.join(".")),
                name: tab.name.clone(),
            },
            table_rid: tab.rid.clone(),
            new_snapshot_id: next_state.current_snapshot_id.or(current_snapshot),
            metadata_location,
        });
    }

    tx.commit()
        .await
        .map_err(|err| ApiError::Internal(format!("commit tx: {err}")))?;

    if !build_rid.is_empty() {
        audit::transaction_commit(actor, &build_rid, committed.len());
    }
    metrics::record_rest_request(
        "POST",
        "/iceberg/v1/transactions/commit",
        StatusCode::OK.as_u16(),
    );
    metrics::FOUNDRY_TRANSACTIONS_TOTAL
        .with_label_values(&["commit"])
        .inc();

    Ok(Json(MultiTableCommitResponse { committed }))
}

fn parse_actor(principal: &AuthenticatedPrincipal) -> Uuid {
    Uuid::parse_str(&principal.subject).unwrap_or_else(|_| Uuid::nil())
}

fn delta_kind_label(delta: &schema_strict::SchemaDelta) -> &'static str {
    match delta {
        schema_strict::SchemaDelta::AddedColumn { .. } => "added-column",
        schema_strict::SchemaDelta::DroppedColumn { .. } => "dropped-column",
        schema_strict::SchemaDelta::ChangedColumnType { .. } => "changed-column-type",
        schema_strict::SchemaDelta::ChangedColumnRequired { .. } => "changed-column-required",
    }
}

async fn rollback_with(
    tx: sqlx::Transaction<'_, sqlx::Postgres>,
    build_rid: &str,
    actor: Uuid,
    table_rid: &str,
    conflicting_with: ConflictKind,
    reason: String,
) -> ApiError {
    let _ = tx.rollback().await;
    metrics::COMMIT_CONFLICTS_TOTAL
        .with_label_values(&[conflicting_with.as_str()])
        .inc();
    audit::transaction_conflict(actor, build_rid, table_rid, conflicting_with.as_str());
    ApiError::Retryable {
        table_rid: table_rid.to_string(),
        reason,
        conflicting_with: conflicting_with.as_str().to_string(),
    }
}

#[derive(Default)]
struct AppliedUpdates {
    current_snapshot_id: Option<i64>,
}

async fn apply_updates_in_tx(
    tx: &mut sqlx::Transaction<'_, sqlx::Postgres>,
    table_id: Uuid,
    current_schema: &Value,
    updates: &[Value],
) -> Result<AppliedUpdates, sqlx::Error> {
    let mut next_schema = current_schema.clone();
    let mut next_props: serde_json::Map<String, Value> = sqlx::query(
        "SELECT properties FROM iceberg_tables WHERE id = $1",
    )
    .bind(table_id)
    .fetch_one(&mut **tx)
    .await?
    .try_get::<Value, _>("properties")?
    .as_object()
    .cloned()
    .unwrap_or_default();
    let mut new_snapshot_id: Option<i64> = None;

    for update in updates {
        let action = update.get("action").and_then(Value::as_str).unwrap_or_default();
        match action {
            "add-schema" => {
                if let Some(schema) = update.get("schema").cloned() {
                    next_schema = schema;
                }
            }
            "set-properties" => {
                if let Some(updates_map) = update.get("updates").and_then(Value::as_object) {
                    for (k, v) in updates_map.iter() {
                        next_props.insert(k.clone(), v.clone());
                    }
                }
            }
            "remove-properties" => {
                if let Some(removals) = update.get("removals").and_then(Value::as_array) {
                    for k in removals.iter().filter_map(|v| v.as_str()) {
                        next_props.remove(k);
                    }
                }
            }
            "add-snapshot" => {
                if let Some(snap) = update.get("snapshot") {
                    let snapshot_id = snap
                        .get("snapshot-id")
                        .and_then(Value::as_i64)
                        .unwrap_or(Utc::now().timestamp_millis());
                    let parent_id = snap.get("parent-snapshot-id").and_then(Value::as_i64);
                    let sequence_number = snap
                        .get("sequence-number")
                        .and_then(Value::as_i64)
                        .unwrap_or(1);
                    let manifest_list = snap
                        .get("manifest-list")
                        .and_then(Value::as_str)
                        .unwrap_or_default()
                        .to_string();
                    let summary = snap
                        .get("summary")
                        .cloned()
                        .unwrap_or_else(|| Value::Object(serde_json::Map::new()));
                    let operation = summary
                        .get("operation")
                        .and_then(Value::as_str)
                        .unwrap_or("append")
                        .to_string();
                    let schema_id = snap
                        .get("schema-id")
                        .and_then(Value::as_i64)
                        .unwrap_or(0) as i32;
                    sqlx::query(
                        r#"
                        INSERT INTO iceberg_snapshots
                          (table_id, snapshot_id, parent_snapshot_id, sequence_number,
                           operation, manifest_list_location, summary, schema_id, timestamp_ms)
                        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
                        ON CONFLICT (table_id, snapshot_id) DO NOTHING
                        "#,
                    )
                    .bind(table_id)
                    .bind(snapshot_id)
                    .bind(parent_id)
                    .bind(sequence_number)
                    .bind(operation)
                    .bind(manifest_list)
                    .bind(&summary)
                    .bind(schema_id)
                    .bind(Utc::now().timestamp_millis())
                    .execute(&mut **tx)
                    .await?;
                    new_snapshot_id = Some(snapshot_id);
                    sqlx::query(
                        "UPDATE iceberg_tables SET current_snapshot_id = $2 WHERE id = $1",
                    )
                    .bind(table_id)
                    .bind(snapshot_id)
                    .execute(&mut **tx)
                    .await?;
                }
            }
            _ => {}
        }
    }

    sqlx::query(
        r#"
        UPDATE iceberg_tables
        SET schema_json = $2,
            properties = $3,
            updated_at = NOW()
        WHERE id = $1
        "#,
    )
    .bind(table_id)
    .bind(&next_schema)
    .bind(&Value::Object(next_props))
    .execute(&mut **tx)
    .await?;

    Ok(AppliedUpdates {
        current_snapshot_id: new_snapshot_id,
    })
}

async fn next_metadata_version_in_tx(
    tx: &mut sqlx::Transaction<'_, sqlx::Postgres>,
    table_id: Uuid,
) -> Result<i64, ApiError> {
    let row: (Option<i32>,) =
        sqlx::query_as("SELECT MAX(version) FROM iceberg_table_metadata_files WHERE table_id = $1")
            .bind(table_id)
            .fetch_one(&mut **tx)
            .await
            .map_err(ApiError::from)?;
    Ok(row.0.unwrap_or(0) as i64)
}

#[allow(unused)]
fn unused_metadata_helpers(_: &metadata::TableMetadata, _: &snapshot::Snapshot) {}
