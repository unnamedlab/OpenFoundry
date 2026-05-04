//! Verifies the legacy `workflows`-row migration step in
//! `migrations/20260504000080_schedules_init.sql`. We seed a workflow
//! row with `trigger_type='cron'`, run the migration, and assert the
//! corresponding `schedules` row was created with a well-formed
//! `trigger_json` Time-trigger and a PipelineBuildTarget `target_json`.
//!
//! Gated behind `it-postgres` because it boots a real Postgres
//! container; included in the default `cargo test` run only when the
//! feature is enabled.

#![cfg(feature = "it-postgres")]

use serde_json::Value;
use sqlx::PgPool;
use testing::containers::boot_postgres;
use uuid::Uuid;

const SCHEDULE_MIGRATION: &str = include_str!("../migrations/20260504000080_schedules_init.sql");

const LEGACY_WORKFLOWS_DDL: &str = "
CREATE TABLE IF NOT EXISTS workflows (
    id               UUID PRIMARY KEY,
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    owner_id         UUID NOT NULL,
    status           TEXT NOT NULL DEFAULT 'active',
    trigger_type     TEXT NOT NULL,
    trigger_config   JSONB NOT NULL DEFAULT '{}',
    steps            JSONB NOT NULL DEFAULT '[]',
    next_run_at      TIMESTAMPTZ,
    last_triggered_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
)";

async fn apply_dependencies(pool: &PgPool) {
    sqlx::query("CREATE EXTENSION IF NOT EXISTS pgcrypto")
        .execute(pool)
        .await
        .expect("pgcrypto");
    sqlx::raw_sql(LEGACY_WORKFLOWS_DDL)
        .execute(pool)
        .await
        .expect("legacy workflows DDL");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn legacy_cron_workflow_migrates_into_trigger_json() {
    let (_container, pool, _url) = boot_postgres().await;
    apply_dependencies(&pool).await;

    let workflow_id = Uuid::now_v7();
    sqlx::query(
        "INSERT INTO workflows (id, name, description, owner_id, status,
                                trigger_type, trigger_config)
         VALUES ($1, 'legacy daily', 'pre-existing cron workflow',
                 $2, 'active', 'cron',
                 '{\"cron\": \"0 9 * * 1\", \"time_zone\": \"America/New_York\"}')",
    )
    .bind(workflow_id)
    .bind(Uuid::now_v7())
    .execute(&pool)
    .await
    .unwrap();

    // Apply the schedule migration. Its embedded DO block walks the
    // workflows table and seeds `schedules`.
    sqlx::raw_sql(SCHEDULE_MIGRATION)
        .execute(&pool)
        .await
        .expect("schedule migration");

    // The legacy row should now appear in `schedules`.
    let row = sqlx::query_as::<_, (Uuid, String, Value, Value)>(
        "SELECT id, name, trigger_json, target_json FROM schedules
         WHERE name = 'legacy daily'",
    )
    .fetch_one(&pool)
    .await
    .expect("migrated row");

    let (_id, name, trigger_json, target_json) = row;
    assert_eq!(name, "legacy daily");

    // trigger_json shape: { kind: { time: { cron, time_zone, flavor } } }
    let time = trigger_json
        .get("kind")
        .and_then(|k| k.get("time"))
        .expect("time leaf");
    assert_eq!(time["cron"].as_str(), Some("0 9 * * 1"));
    assert_eq!(time["time_zone"].as_str(), Some("America/New_York"));
    assert_eq!(time["flavor"].as_str(), Some("UNIX_5"));

    // target_json shape: { kind: { pipeline_build: { pipeline_rid, build_branch } } }
    let pipeline_build = target_json
        .get("kind")
        .and_then(|k| k.get("pipeline_build"))
        .expect("pipeline_build target");
    assert!(
        pipeline_build["pipeline_rid"]
            .as_str()
            .unwrap_or_default()
            .starts_with("ri.foundry.main.pipeline.")
    );
    assert_eq!(pipeline_build["build_branch"].as_str(), Some("master"));
}
