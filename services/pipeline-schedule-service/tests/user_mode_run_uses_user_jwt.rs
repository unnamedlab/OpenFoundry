//! USER-mode schedules forward the caller's JWT verbatim. Cron / Event
//! triggers, which carry no caller JWT, fall back to the build
//! service's static auth header (dispatcher returns `None`); the
//! manual `:run-now` path supplies a JWT, and the dispatcher must use
//! it.

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
async fn manual_dispatch_in_user_mode_propagates_user_jwt() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let schedule = common::create_pipeline_build_schedule(&pool, "user-dispatch").await;

    let build = Arc::new(PrincipalCapturingClient::default());
    let notify = Arc::new(common::StubNotificationClient::default());
    let dispatcher = Dispatcher::new(
        pool.clone(),
        build.clone(),
        notify,
        DispatcherConfig::default(),
    );

    dispatcher
        .dispatch(
            &schedule,
            DispatchTrigger::Manual {
                requested_by: Uuid::now_v7(),
                user_jwt: Some("user.jwt.value".into()),
            },
        )
        .await
        .unwrap();

    let captured = build.captured.lock().unwrap();
    assert_eq!(captured.len(), 1);
    match &captured[0] {
        Some(RunAsPrincipal::UserJwt(jwt)) => assert_eq!(jwt, "user.jwt.value"),
        other => panic!("expected UserJwt, got {other:?}"),
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn cron_dispatch_in_user_mode_does_not_attach_principal() {
    let (_container, pool) = common::boot_with_schedules_schema().await;
    let schedule = common::create_pipeline_build_schedule(&pool, "cron-no-jwt").await;

    let build = Arc::new(PrincipalCapturingClient::default());
    let notify = Arc::new(common::StubNotificationClient::default());
    let dispatcher = Dispatcher::new(
        pool.clone(),
        build.clone(),
        notify,
        DispatcherConfig::default(),
    );

    dispatcher
        .dispatch(
            &schedule,
            DispatchTrigger::Cron {
                fired_at: chrono::Utc::now(),
            },
        )
        .await
        .unwrap();

    let captured = build.captured.lock().unwrap();
    assert_eq!(captured.len(), 1);
    assert!(captured[0].is_none(), "Cron USER dispatch must omit per-call principal");
}
