# CLAUDE.md — libs/ai-kernel-go

Shared library for AI/LLM functionality used by `agent-runtime-service`,
`ai-evaluation-service`, and `llm-catalog-service`. Total ~7.7k LOC.

## Where to look first

| Concern | Open this |
|---|---|
| Multi-provider LLM gateway | `domain/llm/gateway.go` |
| Streaming + completion runtime | `domain/llm/runtime.go` |
| Agent execution loop | `domain/agents/executor.go` |
| Chat HTTP surface | `handlers/chat.go` + `handlers/chat_runtime.go` |
| Agent CRUD/HTTP | `handlers/agents.go` |
| Knowledge base / RAG | `handlers/knowledge.go` + `handlers/knowledge_store.go` |
| Tool calling | `handlers/tools.go` |
| Prompts library | `handlers/prompts.go` |
| Audit/governance hook | `handlers/purpose_checkpoint.go` |

## Files to handle with care

| File | Lines | Notes |
|---|---:|---|
| `domain/agents/executor.go` | 1188 | Agent ReAct loop, tool routing |
| `handlers/chat_runtime.go` | 1172 | Streaming SSE/chat completions; the big handlers `CreateChatCompletion`, `AskCopilot`, `BenchmarkProviders` live here and are individually >200 lines |

Use `grep -n 'func ' file.go` to navigate by symbol.

## Provider integration

LLM provider clients are abstracted behind `domain/llm/gateway.go`. To
add a provider:

1. Implement the gateway interface.
2. Register it in the runtime's provider map.
3. Add config in the consuming service's `internal/config/`, not here
   (this lib must stay provider-agnostic).

## Audit and PII

`handlers/purpose_checkpoint.go` is the **mandatory** entry point for
prompts and outputs that need the audit trail. Don't bypass it for
"internal" calls — every chat completion in production goes through it.

## Testing

```sh
go test ./libs/ai-kernel-go/...
```

Provider calls are mocked by default; live provider tests go in
consumer services with the `integration` build tag.
