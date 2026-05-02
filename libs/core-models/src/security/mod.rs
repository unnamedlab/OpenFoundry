//! Security primitives shared across services that enforce data
//! classification.
//!
//! Currently exposes the [`marking`] module: the canonical
//! [`MarkingId`] / [`EffectiveMarking`] / [`MarkingSource`] types used
//! by the catalog, dataset versioning, pipeline build and ML services
//! to track inherited classifications end-to-end.

pub mod marking;

pub use marking::{EffectiveMarking, InvalidMarkingId, MarkingId, MarkingSource};
