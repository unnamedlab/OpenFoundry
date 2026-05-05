#[allow(dead_code)]
#[path = "../../../../libs/ml-kernel/src/models/deployment.rs"]
pub mod deployment;

// Prediction model absorbed from the retired `model-serving-service`
// and `model-inference-history-service` (S8 consolidation, ADR-0030).
// Routed through `libs/ml-kernel`, the same kernel the legacy crates
// used to re-export.
#[allow(dead_code)]
#[path = "../../../../libs/ml-kernel/src/models/prediction.rs"]
pub mod prediction;

// `model-evaluation-service` absorbed (S8 consolidation, ADR-0030).
// Its scaffolding re-exported `ml-kernel/models/deployment.rs` already
// re-exported above; no additional model module is required.
