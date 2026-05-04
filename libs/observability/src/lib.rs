//! Shared observability primitives for OpenFoundry services.
//!
//! Today this crate exposes one surface:
//!
//!   * [`cost_model`] — the Foundry compute-seconds cost table for
//!     media-set transformations. Every consumer that bills, meters,
//!     or visualises media usage reads from the same table so a doc
//!     update lands in one place and a single snapshot test
//!     (`services/media-sets-service/tests/cost_model_matches_table.rs`)
//!     enforces drift never sneaks past CI.
//!
//! The crate stays dependency-light (just `serde` for the few `Serialize`
//! / `Deserialize` derives the cost rows need) so any service can pull
//! it in without inflating its compile graph.

pub mod cost_model;

pub use cost_model::{
    COST_TABLE, CostEntry, DOWNLOAD_STREAM, SchemaKind, charge_compute_seconds, entries_for,
    entry, rate_per_gb,
};
