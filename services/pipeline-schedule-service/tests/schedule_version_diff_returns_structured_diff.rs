//! Pure-logic verification of the `version_store::diff_versions`
//! helper that backs `GET /v1/schedules/{rid}/versions/diff`.
//!
//! Asserts the diff contract the UI consumes:
//!
//!   * scalar `name` / `description` changes surface as
//!     `{ before, after }` objects;
//!   * trigger / target diffs are `[ { path, before, after } ]` lists
//!     with paths walking the JSON tree (e.g. `kind.time.cron`);
//!   * unchanged subtrees are elided so the UI does not render no-op
//!     rows.

use chrono::Utc;
use pipeline_schedule_service::domain::version_store::{
    ScheduleVersion, diff_versions, json_diff,
};
use serde_json::json;
use uuid::Uuid;

fn version(version: i32, name: &str, trigger: serde_json::Value) -> ScheduleVersion {
    ScheduleVersion {
        id: Uuid::now_v7(),
        schedule_id: Uuid::nil(),
        version,
        name: name.to_string(),
        description: format!("v{version}"),
        trigger_json: trigger,
        target_json: json!({"kind": {"pipeline_build": {"pipeline_rid": "ri.x", "build_branch": "master"}}}),
        edited_by: "tester".into(),
        edited_at: Utc::now(),
        comment: "".into(),
    }
}

#[test]
fn diff_with_only_cron_change_emits_single_trigger_entry() {
    let from = version(
        1,
        "daily",
        json!({"kind": {"time": {"cron": "0 9 * * *", "time_zone": "UTC", "flavor": "UNIX_5"}}}),
    );
    let to = version(
        2,
        "daily",
        json!({"kind": {"time": {"cron": "0 12 * * *", "time_zone": "UTC", "flavor": "UNIX_5"}}}),
    );
    let diff = diff_versions(&from, &to);
    assert!(diff.name_diff.is_none());
    assert!(diff.description_diff.is_some());
    assert_eq!(diff.trigger_diff.len(), 1);
    assert_eq!(diff.trigger_diff[0].path, "kind.time.cron");
    assert_eq!(diff.trigger_diff[0].before, json!("0 9 * * *"));
    assert_eq!(diff.trigger_diff[0].after, json!("0 12 * * *"));
    assert!(diff.target_diff.is_empty());
}

#[test]
fn diff_with_name_change_surfaces_field_change() {
    let from = version(1, "old name", json!({}));
    let to = version(2, "new name", json!({}));
    let diff = diff_versions(&from, &to);
    let nd = diff.name_diff.expect("name change must surface");
    assert_eq!(nd.before, "old name");
    assert_eq!(nd.after, "new name");
}

#[test]
fn diff_emits_path_for_nested_addition() {
    let before = json!({"kind": {"time": {"cron": "0 9 * * *"}}});
    let after = json!({
        "kind": {
            "time": {"cron": "0 9 * * *", "time_zone": "America/New_York"}
        }
    });
    let entries = json_diff(&before, &after);
    assert_eq!(entries.len(), 1);
    assert_eq!(entries[0].path, "kind.time.time_zone");
    assert!(entries[0].before.is_null());
}

#[test]
fn diff_walks_full_tree_for_kind_swap() {
    // Switching a Time trigger to an Event trigger touches multiple
    // branches; the diff should call them all out so the UI can paint
    // each one.
    let before = json!({"kind": {"time": {"cron": "0 9 * * *", "time_zone": "UTC", "flavor": "UNIX_5"}}});
    let after = json!({"kind": {"event": {"type": "DATA_UPDATED", "target_rid": "ri.x"}}});
    let entries = json_diff(&before, &after);
    let paths: Vec<&str> = entries.iter().map(|e| e.path.as_str()).collect();
    assert!(paths.iter().any(|p| p.starts_with("kind.time")));
    assert!(paths.iter().any(|p| p.starts_with("kind.event")));
}

#[test]
fn diff_returns_empty_for_identical_versions() {
    let v = version(1, "same", json!({"kind": {"time": {"cron": "0 9 * * *"}}}));
    let mut v2 = v.clone();
    v2.version = 2;
    v2.description = v.description.clone();
    let diff = diff_versions(&v, &v2);
    assert!(diff.name_diff.is_none());
    assert!(diff.description_diff.is_none());
    assert!(diff.trigger_diff.is_empty());
    assert!(diff.target_diff.is_empty());
}
