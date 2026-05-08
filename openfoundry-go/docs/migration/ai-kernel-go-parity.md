# ai-kernel-go parity slice

This slice advances the `ai-kernel-bound` block by porting the Rust `libs/ai-kernel/src/models/**` wire DTOs into `openfoundry-go/libs/ai-kernel-go/models/**` and wiring a minimal shell service proof point in `services/llm-catalog-service`.

## Rust model inventory used by shell services

`services/llm-catalog-service/src/{models,handlers,domain}.rs` re-export the shared Rust AI kernel modules, so its wire surface is the AI kernel model inventory below. `services/application-composition-service` keeps its own `PrimaryItem`, `CreatePrimaryRequest`, `SecondaryItem`, and `CreateSecondaryRequest` models and does not import AI kernel DTOs in this slice.

Ported AI kernel structs/enums/constants:

- `provider.rs`: `ProviderRoutingRules`, `ProviderHealthState`, `LlmProvider`, `ListProvidersResponse`, `CreateProviderRequest`, `UpdateProviderRequest`, and provider defaults.
- `agent.rs`: `AgentMemorySnapshot`, `AgentPlanStep`, `AgentExecutionTrace`, `AgentDefinition`, list/create/update/execute request and response structs, and agent defaults.
- `conversation.rs`: guardrail, attachment, chat message, usage, routing, cache, conversation, chat completion, copilot, guardrail evaluation, provider benchmark structs, and conversation defaults.
- `knowledge_base.rs`: `KnowledgeChunk`, `KnowledgeDocument`, `KnowledgeBase`, `KnowledgeSearchResult`, list/create/update/search request and response structs, and RAG defaults.
- `prompt_template.rs`: `PromptVersion`, `PromptTemplate`, list/create/update/render request and response structs, and prompt defaults.
- `tool.rs`: `ToolDefinition`, list/create/update request structs, supported execution modes, and tool defaults.
- `overview.go` covers the overview payload already exposed by the Go handler slice.

No Rust enums are present in `libs/ai-kernel/src/models/**`; string fields carry enum-like values such as provider type, API mode, tool execution mode, prompt category, status, and network scope.

## Completed Go port decisions

- Go JSON tags are snake_case to match Rust serde field names.
- `#[serde(default)]` and `#[serde(default = "...")]` fields now have Go unmarshal defaults where callers need Rust-compatible decoded request objects.
- Optional Rust fields (`Option<T>`) remain Go pointers and serialize as `null` when unset.
- Rust default collections are represented as concrete slices after request unmarshal; callers that construct response structs directly should initialize empty slices when they need `[]` instead of JSON `null`.
- Dynamic Rust `serde_json::Value` fields use `json.RawMessage`, with `{}` as the request default where Rust has `default_json_object` or generic `Value` defaults.

## End-to-end shell proof

`llm-catalog-service` now exposes `GET /api/v1/kernel-defaults`. It does not fabricate catalog rows; it returns real defaults and supported tool modes from `libs/ai-kernel-go/models`, proving the shell service can compile, route, marshal, and test through the ported model package.

## Remaining slices

1. **handlers**
   - Continue auditing `libs/ai-kernel-go/handlers/**` against `libs/ai-kernel/src/handlers/**` after the DTO port.
   - Replace any request-local fallback logic that is now covered by model unmarshal defaults.
   - Add route-level contract tests that pin request defaults and response shape for chat, providers, prompts, tools, knowledge, and agents.

2. **domain/llm**
   - Finish parity on provider selection, fallback, semantic cache behavior, guardrail integration, provider benchmarks, and runtime error semantics.
   - Verify private-network routing and modality filtering against Rust gateway/provider/runtime code.

3. **domain/agents**
   - Complete planner/executor/memory parity around plan-act-observe traces, max-iteration handling, tool-result observations, and memory updates.
   - Add tests that execute an agent through real in-process domain components rather than fixture-only traces.

4. **domain/rag**
   - Finish chunker, embedder, indexer, retriever, and vector-store parity.
   - Pin chunk IDs, score thresholds, metadata defaults, and deterministic embedding behavior against Rust tests/fixtures.
