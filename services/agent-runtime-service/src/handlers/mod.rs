// S8.1.b ADR-0030 — `tool-registry-service` consolidated into this service.
// `tools` now exposes the tool registry HTTP handlers previously owned
// by the deleted `tool-registry-service`. The source-of-truth still
// lives in `libs/ai-kernel/src/handlers/tools.rs` (until ai-kernel is
// folded into this crate in a follow-up R-prompt).

pub mod agents;
pub mod tools;
