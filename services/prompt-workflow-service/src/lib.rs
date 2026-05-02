//! `prompt-workflow-service` — substrate-only library.
//!
//! Surfaces [`ai_events`] which mirrors the publisher constants used
//! by `agent-runtime-service`. Both services emit to the same topic
//! `ai.events.v1` (S5.3.a); event `kind` distinguishes producers.

pub mod ai_events;
