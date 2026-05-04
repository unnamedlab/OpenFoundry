#[allow(dead_code)]
#[path = "../../../pipeline-authoring-service/src/domain/engine/mod.rs"]
pub mod engine;
#[path = "../../../pipeline-authoring-service/src/domain/executor.rs"]
pub mod executor;
// `engine/mod.rs` dispatches into `crate::domain::media_nodes` for the
// Foundry media node kinds added in P1.4. Re-root the same module here
// so the path-reuse stays compileable in this service's crate too.
pub mod aip;
pub mod aip_http_client;
pub mod build_client;
pub mod cron_dispatch;
pub mod cron_registrar;
pub mod dispatcher;
pub mod event_listener;
pub mod metrics;
pub mod outbox_events;
pub mod troubleshoot;
#[allow(dead_code)]
#[path = "../../../lineage-service/src/domain/lineage/mod.rs"]
pub mod lineage;
#[allow(dead_code)]
#[path = "../../../pipeline-authoring-service/src/domain/media_nodes.rs"]
pub mod media_nodes;
pub mod notification_client;
pub mod run_store;
pub mod schedule;
pub mod schedule_store;
pub mod service_principal_store;
pub mod trigger;
pub mod trigger_engine;
pub mod version_store;
pub mod workflow;
