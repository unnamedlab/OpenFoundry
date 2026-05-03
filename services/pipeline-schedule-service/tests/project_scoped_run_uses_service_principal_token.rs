//! Per the Foundry doc § "Project scope":
//!
//!   "Project-scoped mode … is more consistent, since the schedule is
//!    run independently of the user's permissions."
//!
//! The dispatcher must therefore propagate a service-principal token
//! (not the user's JWT) when `scope_kind == PROJECT_SCOPED`. We assert
//! that contract end-to-end through `BuildServiceClient::create_build_as`
//! by capturing the principal the dispatcher hands off.

#![cfg(feature = "it-postgres")]

mod common;

use std::sync::{Arc, Mutex};

use async_trait::async_trait;
use pipeline_schedule_service::domain::build_client::{
    BuildAttemptOutcome, BuildServiceClient, CreateBuildPayload, RunAsPrincipal,
};
use pipeline_schedule_service::domain::dispatcher::{
    Dispatcher, DispatcherConfig, DispatchTrigger,
};
use pipeline_schedule_service::domain::run_store::RunOutcome;
use pipeline_schedule_service::domain::{schedule_store, service_principal_store};
use uuid::Uuid;

#[derive(Default, Clone)]
struct PrincipalCapturingClient {
    captured: Arc<Mutex<Vec<Option<RunAsPrincipal>>>>,
}

#[async_trait]
impl BuildServiceClient for PrincipalCapturingClient {
    async fn create_build(&self, _req: &CreateBuildPayload) -> BuildAttemptOutcome {
        self.captured.lock().unwrap().push(None);
        BuildAttemptOutcome::Started {
            build_rid: "ri.foundry.main.build.42".into(),
        }
    }
    async fn create_build_as(
        &self,
        _req: &CreateBuildPayload,
        principal: &RunAsPrincipal,
    ) -> BuildAttemptOutcome {
        self.captured.lock().unwrap().push(Some(principal.clone()));
        BuildAttemptOutcome::Started {
            build_rid: "ri.foundry.main.build.42".into(),
        }
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn project_scoped_dispatch_propagates_service_principal_token() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let schedule = common::create_pipeline_build_schedule(&pool, "ps-dispatch").await;

    // Mint a service principal and convert the schedule to PROJECT_SCOPED.
    let sp = service_principal_store::create(
        &pool,
        service_principal_store::CreateServicePrincipal {
            display_name: "ps-test-runner".into(),
            project_scope_rids: vec!["ri.foundry.main.project.alpha".into()],
            clearances: vec!["INTERNAL".into()],
            created_by: Uuid::nil().to_string(),
        },
    )
    .await
    .unwrap();
    let updated = schedule_store::convert_to_project_scope(
        &pool,
        &schedule.rid,
        vec!["ri.foundry.main.project.alpha".into()],
        sp.id,
    )
    .await
    .unwrap();

    let build = Arc::new(PrincipalCapturingClient::default());
    let notify = Arc::new(common::StubNotificationClient::default());
    let dispatcher = Dispatcher::new(
        pool.clone(),
        build.clone(),
        notify,
        DispatcherConfig::default(),
    );

    let report = dispatcher
        .dispatch(
            &updated,
            DispatchTrigger::Cron {
                fired_at: chrono::Utc::now(),
            },
        )
        .await
        .unwrap();
    assert_eq!(report.run.unwrap().outcome, RunOutcome::Succeeded);

    let captured = build.captured.lock().unwrap();
    assert_eq!(captured.len(), 1);
    match &captured[0] {
        Some(RunAsPrincipal::ServicePrincipalToken(token)) => {
            assert_eq!(token, &sp.id.to_string());
        }
        other => panic!("expected service principal token, got {other:?}"),
    }
}
