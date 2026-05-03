//! Direct test of the schedule_store::convert_to_project_scope path.
//! The HTTP handler enforces the Cedar policy that requires `manage`
//! on every target project; here we exercise the underlying
//! transition + the constraint that PROJECT_SCOPED schedules MUST
//! carry both `service_principal_id` and a non-empty
//! `project_scope_rids`.

#![cfg(feature = "it-postgres")]

mod common;

use pipeline_schedule_service::domain::trigger::ScheduleScopeKind;
use pipeline_schedule_service::domain::{schedule_store, service_principal_store};
use uuid::Uuid;

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn convert_to_project_scope_flips_kind_and_persists_principal_id() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let schedule = common::create_pipeline_build_schedule(&pool, "convert").await;
    assert_eq!(schedule.scope_kind, ScheduleScopeKind::User);

    let sp = service_principal_store::create(
        &pool,
        service_principal_store::CreateServicePrincipal {
            display_name: "convert-test".into(),
            project_scope_rids: vec![
                "ri.foundry.main.project.alpha".into(),
                "ri.foundry.main.project.beta".into(),
            ],
            clearances: vec!["INTERNAL".into()],
            created_by: Uuid::nil().to_string(),
        },
    )
    .await
    .unwrap();

    let updated = schedule_store::convert_to_project_scope(
        &pool,
        &schedule.rid,
        sp.project_scope_rids.clone(),
        sp.id,
    )
    .await
    .unwrap();
    assert_eq!(updated.scope_kind, ScheduleScopeKind::ProjectScoped);
    assert_eq!(updated.project_scope_rids, sp.project_scope_rids);
    assert_eq!(updated.service_principal_id, Some(sp.id));
    assert_eq!(updated.run_as_user_id, None);
    assert_eq!(updated.version, schedule.version + 1);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn convert_with_empty_scope_rejected_by_check_constraint() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let schedule = common::create_pipeline_build_schedule(&pool, "empty-rids").await;
    let sp = service_principal_store::create(
        &pool,
        service_principal_store::CreateServicePrincipal {
            display_name: "convert-empty".into(),
            project_scope_rids: vec![],
            clearances: vec![],
            created_by: Uuid::nil().to_string(),
        },
    )
    .await
    .unwrap();

    // The store accepts `vec![]` but the CHECK constraint on the
    // schedules table rejects it. The error type is opaque, so just
    // assert the call fails.
    let res = schedule_store::convert_to_project_scope(&pool, &schedule.rid, vec![], sp.id).await;
    assert!(
        res.is_err(),
        "PROJECT_SCOPED with empty project_scope_rids must be rejected"
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn convert_back_to_user_clears_service_principal() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let schedule = common::create_pipeline_build_schedule(&pool, "round-trip").await;
    let sp = service_principal_store::create(
        &pool,
        service_principal_store::CreateServicePrincipal {
            display_name: "rt".into(),
            project_scope_rids: vec!["ri.foundry.main.project.alpha".into()],
            clearances: vec![],
            created_by: Uuid::nil().to_string(),
        },
    )
    .await
    .unwrap();

    schedule_store::convert_to_project_scope(
        &pool,
        &schedule.rid,
        sp.project_scope_rids.clone(),
        sp.id,
    )
    .await
    .unwrap();

    let user_id = Uuid::now_v7();
    let back = schedule_store::convert_to_user_scope(&pool, &schedule.rid, user_id)
        .await
        .unwrap();
    assert_eq!(back.scope_kind, ScheduleScopeKind::User);
    assert_eq!(back.run_as_user_id, Some(user_id));
    assert_eq!(back.service_principal_id, None);
    assert!(back.project_scope_rids.is_empty());
}
