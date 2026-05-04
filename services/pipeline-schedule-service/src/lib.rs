//! Library face of `pipeline-schedule-service`.
//!
//! Exposes only the **new** Foundry-parity trigger / schedule plumbing
//! introduced by the schedules redesign so the integration tests in
//! `tests/*.rs` can drive it without going through HTTP. The binary
//! (`src/main.rs`) keeps its own module tree for the path-imported
//! shims it shares with `pipeline-authoring-service`; this lib is
//! intentionally narrower.

pub mod domain {
    pub mod aip;
    pub mod aip_http_client;
    pub mod build_client;
    pub mod cron_dispatch;
    pub mod cron_registrar;
    pub mod dispatcher;
    pub mod event_listener;
    pub mod metrics;
    pub mod notification_client;
    pub mod outbox_events;
    pub mod run_store;
    pub mod schedule_store;
    pub mod service_principal_store;
    pub mod trigger;
    pub mod trigger_engine;
    pub mod troubleshoot;
    pub mod version_store;
}
