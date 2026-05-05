#[path = "../../../../libs/ml-kernel/src/domain/drift.rs"]
pub mod drift;

// Predictions domain absorbed from the retired `model-serving-service`
// and `model-inference-history-service` (S8 consolidation, ADR-0030).
// Routed through `libs/ml-kernel`, the same kernel the legacy crates
// used to re-export.
#[path = "../../../../libs/ml-kernel/src/domain/predictions.rs"]
pub mod predictions;

// `model-evaluation-service` absorbed (S8 consolidation, ADR-0030).
// Its scaffolding re-exported `ml-kernel/domain/drift.rs` already
// re-exported above; no additional domain module is required.
