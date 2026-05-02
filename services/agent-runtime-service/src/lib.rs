//! `agent-runtime-service` — substrate-only library.
//!
//! Surfaces the [`ai_events`] module that pins the Kafka topic name,
//! consumer/producer constants, the AI-event envelope and event
//! kinds. The producer loop (`DataPublisher::publish`) lands in a
//! follow-up PR per S5.3.a — same handler-by-handler pattern as
//! S2.5.b/S3.2.d/S4.5.

pub mod ai_events;
