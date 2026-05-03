// S8.1.b ADR-0030 — absorbido desde tool-registry-service
//
// Tool registry HTTP handlers (list/create/update). Source-of-truth
// lives in `libs/ai-kernel/src/handlers/tools.rs`; the `#[path]`
// re-export keeps the dispatch in-process so the agent runtime no
// longer needs the retired `tool_registry_service_url`.

#[path = "../../../../libs/ai-kernel/src/handlers/tools.rs"]
mod shared;
pub use shared::*;
