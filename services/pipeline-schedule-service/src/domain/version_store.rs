//! Read access to `schedule_versions` plus a structured diff between
//! two versions. The version snapshots themselves are written by the
//! `schedules_version_snapshot` BEFORE-UPDATE trigger from P1.

use chrono::{DateTime, Utc};
use serde::Serialize;
use serde_json::Value;
use sqlx::{PgPool, Row, postgres::PgRow};
use uuid::Uuid;

#[derive(Debug, Clone, Serialize)]
pub struct ScheduleVersion {
    pub id: Uuid,
    pub schedule_id: Uuid,
    pub version: i32,
    pub name: String,
    pub description: String,
    pub trigger_json: Value,
    pub target_json: Value,
    pub edited_by: String,
    pub edited_at: DateTime<Utc>,
    pub comment: String,
}

#[derive(Debug, thiserror::Error)]
pub enum VersionError {
    #[error("database error: {0}")]
    Db(#[from] sqlx::Error),
    #[error("version {0} not found for schedule {1}")]
    NotFound(i32, String),
}

pub async fn list_versions(
    pool: &PgPool,
    schedule_id: Uuid,
    limit: i64,
    offset: i64,
) -> Result<Vec<ScheduleVersion>, VersionError> {
    let limit = if limit <= 0 { 50 } else { limit.min(500) };
    let offset = offset.max(0);
    let rows = sqlx::query(
        r#"SELECT id, schedule_id, version, name, description,
                  trigger_json, target_json, edited_by, edited_at, comment
             FROM schedule_versions
            WHERE schedule_id = $1
            ORDER BY version DESC
            LIMIT $2 OFFSET $3"#,
    )
    .bind(schedule_id)
    .bind(limit)
    .bind(offset)
    .fetch_all(pool)
    .await?;
    Ok(rows.iter().map(version_from_row).collect::<Result<_, _>>()?)
}

pub async fn get_version(
    pool: &PgPool,
    schedule_id: Uuid,
    version: i32,
) -> Result<ScheduleVersion, VersionError> {
    let row = sqlx::query(
        r#"SELECT id, schedule_id, version, name, description,
                  trigger_json, target_json, edited_by, edited_at, comment
             FROM schedule_versions
            WHERE schedule_id = $1 AND version = $2"#,
    )
    .bind(schedule_id)
    .bind(version)
    .fetch_optional(pool)
    .await?;
    match row {
        Some(r) => Ok(version_from_row(&r)?),
        None => Err(VersionError::NotFound(version, schedule_id.to_string())),
    }
}

fn version_from_row(row: &PgRow) -> Result<ScheduleVersion, sqlx::Error> {
    Ok(ScheduleVersion {
        id: row.try_get("id")?,
        schedule_id: row.try_get("schedule_id")?,
        version: row.try_get("version")?,
        name: row.try_get("name")?,
        description: row.try_get("description")?,
        trigger_json: row.try_get("trigger_json")?,
        target_json: row.try_get("target_json")?,
        edited_by: row.try_get("edited_by")?,
        edited_at: row.try_get("edited_at")?,
        comment: row.try_get("comment")?,
    })
}

// ---- Structured diff -------------------------------------------------------

/// Diff between two versions as the UI consumes it. Each `*_diff` is a
/// list of `{ path, before, after }` entries; equal subtrees are
/// elided, so an empty list means "no change in that field".
#[derive(Debug, Clone, Serialize)]
pub struct VersionDiff {
    pub schedule_id: Uuid,
    pub from_version: i32,
    pub to_version: i32,
    pub name_diff: Option<FieldChange<String>>,
    pub description_diff: Option<FieldChange<String>>,
    pub trigger_diff: Vec<JsonDiffEntry>,
    pub target_diff: Vec<JsonDiffEntry>,
}

#[derive(Debug, Clone, Serialize)]
pub struct FieldChange<T> {
    pub before: T,
    pub after: T,
}

#[derive(Debug, Clone, Serialize)]
pub struct JsonDiffEntry {
    /// Dotted path into the JSON tree (e.g. `kind.time.cron`).
    pub path: String,
    pub before: Value,
    pub after: Value,
}

/// Compute a structured diff between two ScheduleVersion snapshots.
pub fn diff_versions(from: &ScheduleVersion, to: &ScheduleVersion) -> VersionDiff {
    VersionDiff {
        schedule_id: from.schedule_id,
        from_version: from.version,
        to_version: to.version,
        name_diff: if from.name == to.name {
            None
        } else {
            Some(FieldChange {
                before: from.name.clone(),
                after: to.name.clone(),
            })
        },
        description_diff: if from.description == to.description {
            None
        } else {
            Some(FieldChange {
                before: from.description.clone(),
                after: to.description.clone(),
            })
        },
        trigger_diff: json_diff(&from.trigger_json, &to.trigger_json),
        target_diff: json_diff(&from.target_json, &to.target_json),
    }
}

/// Walk `before` and `after` in parallel, recording every leaf where
/// they disagree. Object subtrees recurse on their union of keys; non-
/// object subtrees produce a single entry at their current path.
pub fn json_diff(before: &Value, after: &Value) -> Vec<JsonDiffEntry> {
    let mut out = Vec::new();
    walk("", before, after, &mut out);
    out
}

fn walk(prefix: &str, before: &Value, after: &Value, out: &mut Vec<JsonDiffEntry>) {
    if before == after {
        return;
    }
    match (before, after) {
        (Value::Object(b), Value::Object(a)) => {
            let mut keys: Vec<&String> = b.keys().chain(a.keys()).collect();
            keys.sort();
            keys.dedup();
            for key in keys {
                let next = if prefix.is_empty() {
                    key.clone()
                } else {
                    format!("{prefix}.{key}")
                };
                let bv = b.get(key).unwrap_or(&Value::Null);
                let av = a.get(key).unwrap_or(&Value::Null);
                walk(&next, bv, av, out);
            }
        }
        _ => out.push(JsonDiffEntry {
            path: prefix.to_string(),
            before: before.clone(),
            after: after.clone(),
        }),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn json_diff_emits_entry_for_changed_leaf() {
        let before = json!({"kind": {"time": {"cron": "0 9 * * *"}}});
        let after = json!({"kind": {"time": {"cron": "0 12 * * *"}}});
        let entries = json_diff(&before, &after);
        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].path, "kind.time.cron");
        assert_eq!(entries[0].before, json!("0 9 * * *"));
        assert_eq!(entries[0].after, json!("0 12 * * *"));
    }

    #[test]
    fn json_diff_emits_entry_for_added_key() {
        let before = json!({"kind": {"time": {"cron": "0 9 * * *"}}});
        let after = json!({
            "kind": {
                "time": {"cron": "0 9 * * *", "time_zone": "America/New_York"}
            }
        });
        let entries = json_diff(&before, &after);
        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].path, "kind.time.time_zone");
        assert_eq!(entries[0].before, Value::Null);
        assert_eq!(entries[0].after, json!("America/New_York"));
    }

    #[test]
    fn json_diff_returns_empty_for_equal_trees() {
        let val = json!({"a": 1, "b": [1, 2, 3]});
        assert!(json_diff(&val, &val).is_empty());
    }

    #[test]
    fn json_diff_treats_arrays_as_leaves_per_position() {
        let before = json!({"arr": [1, 2, 3]});
        let after = json!({"arr": [1, 2, 4]});
        let entries = json_diff(&before, &after);
        assert_eq!(entries.len(), 1);
        assert_eq!(entries[0].path, "arr");
    }
}
