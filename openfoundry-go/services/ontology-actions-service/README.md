# `ontology-actions-service` (Go)

Sole runtime owner of the consolidated ontology bounded contexts per
[ADR-0030](../../../docs/architecture/adr/ADR-0030-service-consolidation-30-targets.md):

- **actions** — `/api/v1/ontology/actions/*` and inline-edit
- **funnel** — `/api/v1/ontology/funnel/*` and `/storage/insights`
- **functions** — `/api/v1/ontology/functions/*` plus the Foundry
  "Functions on objects → Media" runtime ([`internal/mediafunctions`](internal/mediafunctions/media.go))
- **rules** — `/api/v1/ontology/rules/*`, `/types/{id}/rules`,
  `/objects/{id}/rule-runs`

## Port status

| Component | Status |
|---|---|
| Health (`/health`, `/healthz`) + Prometheus (`/metrics`) | ✅ wired |
| JWT-protected `/api/v1/ontology/*` mount | ✅ wired |
| URL grid (every Rust route mounted with the same path / verb) | ✅ wired |
| `mediafunctions` package (`read_raw`, `ocr`, `extract_text`, `transcribe_audio`, `read_metadata` + `MockRuntime`) | ✅ ported 1:1 with Rust H6 tests |
| Action / funnel / function / rule handlers | 🟡 stubbed — return empty envelopes / 501. Real bodies depend on `libs/ontology-kernel-go` handler slice (in progress) |
| Cassandra writeback path (`apply_object_with_outbox`) | ⏳ pending — gated on `libs/cassandra-kernel-go` |

The same in-process integration tests the Rust crate ships with
(`tests/health.rs::list_action_types_requires_bearer_token` and
`absorbed_routes_require_bearer_token`) are mirrored under
[`internal/server/server_test.go`](internal/server/server_test.go).

## Build & run

```sh
go build -o bin/ontology-actions-service ./services/ontology-actions-service/cmd/ontology-actions-service
go test ./services/ontology-actions-service/...
```

## Configuration

Env vars (defaults match the Rust port):

| Variable | Default |
|---|---|
| `HOST` | `0.0.0.0` |
| `PORT` | `50106` |
| `JWT_SECRET` | (required) |
| `DATABASE_URL` | unset (handlers gated on this when the kernel slice lands) |
| `CASSANDRA_CONTACT_POINTS` | empty → in-memory store fallback (S1.4.a) |
| `AUDIT_SERVICE_URL` | `http://localhost:50115` |
| `DATASET_SERVICE_URL` | `http://localhost:50079` |
| `ONTOLOGY_SERVICE_URL` | `http://localhost:50103` |
| `PIPELINE_SERVICE_URL` | `http://localhost:50081` |
| `AI_SERVICE_URL` | `http://localhost:50127` |
| `NOTIFICATION_SERVICE_URL` | `http://localhost:50114` |
| `CONNECTOR_MANAGEMENT_SERVICE_URL` | `http://localhost:50130` |
| `SEARCH_EMBEDDING_PROVIDER` | `deterministic-hash` |
| `NODE_RUNTIME_COMMAND` | `node` |
