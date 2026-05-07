//! Operational automations subdomain — merged in from the legacy
//! `automation-operations-service` (S8 consolidation; ADR-0030 +
//! `service-consolidation-map.md`).
//!
//! Owns the Postgres-backed saga substrate
//! (`libs/saga::SagaRunner` over the `saga.state` table in this
//! service's bounded-context cluster), the HTTP `POST /api/v1/automations`
//! producer, and the `saga.step.requested.v1` Kafka consumer. The
//! consumer group id is preserved as `automation-operations-service`
//! so changing the deployable does NOT reprocess the topic log.

pub mod domain;
pub mod event;
pub mod handlers;
pub mod models;
pub mod topics;
