//! Saga step graphs registered in this service.
//!
//! Each module here defines one saga's step types (`SagaStep` impls)
//! and their inputs / outputs. The dispatch from a `task_type`
//! string (the saga type carried on `saga.step.requested.v1` events)
//! to a step graph lives in [`super::dispatcher`].

pub mod cleanup_workspace;
pub mod retention_sweep;
