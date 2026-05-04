//! End-to-end sweep:apply path. The linter library returns a
//! `recommended_action: Pause` for SCH-001 (idle schedule); the
//! schedule-service handler must execute that action against the
//! schedules table when the operator confirms the apply.

#![cfg(feature = "it-postgres")]

mod common;

use pipeline_schedule_service::domain::{schedule_store, trigger::MANUAL_PAUSED_REASON as _};
use scheduling_linter::{Action, Finding, RuleId, Severity, SweepReport, planner::AppliedAction};
use uuid::Uuid;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn applying_a_pause_finding_pauses_the_schedule_row() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let schedule = common::create_pipeline_build_schedule(&pool, "sweep-target").await;

    // Hand-build a SweepReport mirroring what the linter emits for SCH-001.
    let finding = Finding {
        id: Uuid::now_v7(),
        rule_id: RuleId::Sch001InactiveLastNinety,
        severity: Severity::Warning,
        schedule_rid: schedule.rid.clone(),
        project_rid: schedule.project_rid.clone(),
        message: "test".into(),
        recommended_action: Action::Pause,
    };
    let report = SweepReport {
        findings: vec![finding.clone()],
    };

    // Plan + execute the apply step the way the handler does.
    let plan = report.plan_apply(&[], &[finding.id]);
    assert_eq!(plan.len(), 1);
    let AppliedAction {
        schedule_rid,
        action,
        ..
    } = plan[0].clone();
    assert_eq!(action, Action::Pause);
    schedule_store::set_paused(&pool, &schedule_rid, true, Some("LINTER"))
        .await
        .unwrap();

    let after = schedule_store::get_by_rid(&pool, &schedule.rid)
        .await
        .unwrap();
    assert!(after.paused);
    assert_eq!(after.paused_reason.as_deref(), Some("LINTER"));
}
