// S8.1.b ADR-0030 — absorbido desde tool-registry-service
//
// Tool registry models (`ToolDefinition`, `CreateToolRequest`,
// `UpdateToolRequest`, `ListToolsResponse`, ...). Source-of-truth
// lives in `libs/ai-kernel/src/models/tool.rs`; the `#[path]`
// re-export keeps the schema co-located with the agent runtime so
// tool dispatch can stay in-process.

#[path = "../../../../libs/ai-kernel/src/models/tool.rs"]
mod shared;
pub use shared::*;
