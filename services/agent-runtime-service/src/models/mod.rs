// S8.1.b ADR-0030 — `tool-registry-service` consolidated into this service.
// `tools` now exposes the tool registry models previously owned by the
// deleted `tool-registry-service`. The source-of-truth still lives in
// `libs/ai-kernel/src/models/tool.rs`.

pub mod agents;
pub mod tools;
