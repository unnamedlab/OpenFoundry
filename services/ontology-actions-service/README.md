# `ontology-actions-service`

> Foundry-style **Action Types** runtime: authoring, validation, execution,
> batched execution, inline edits, what-if branches, applicable-actions
> discovery, attachment uploads and a Prometheus + JSON metrics surface.

The HTTP layer is mounted by `build_router` in `src/lib.rs`; the binary in
`src/main.rs` wires it to a TCP listener, attaches the `tracing` /
`prometheus` middlewares and exposes `/health` + `/metrics`.

Foundry parity references for every endpoint live under
`docs_original_palantir_foundry/foundry-docs` — see
[Foundry mapping](#foundry-mapping) below.

---

## HTTP contract

All routes are mounted under `/api/v1/ontology` and require a Bearer token
issued by `enterprise-auth-service`. `GET /health` and `GET /metrics` stay
open and bypass the JWT layer.

| Method   | Path                                                                                  | Purpose                                                                |
| -------- | ------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `GET`    | `/actions`                                                                            | List action types, filterable by `object_type_id`, `search`, `page`.   |
| `POST`   | `/actions`                                                                            | Create an action type (TASK G/H/J/K config envelope).                  |
| `GET`    | `/actions/{id}`                                                                       | Fetch a single action type.                                            |
| `PUT`    | `/actions/{id}`                                                                       | Update an action type.                                                 |
| `DELETE` | `/actions/{id}`                                                                       | Soft delete for the declarative action type.                           |
| `POST`   | `/actions/{id}/validate`                                                              | Plan + validate without persisting (dry-run).                          |
| `POST`   | `/actions/{id}/execute`                                                               | Execute against a single target.                                       |
| `POST`   | `/actions/{id}/execute-batch`                                                         | Execute against ≤ 20 / ≤ 10 000 targets (TASK M caps).                 |
| `GET`    | `/actions/{id}/metrics?window=30d`                                                    | Aggregated `success/failure/p95` from the Cassandra action log.        |
| `GET`    | `/actions/{id}/what-if`                                                               | List what-if branches for an action.                                   |
| `POST`   | `/actions/{id}/what-if`                                                               | Create a what-if branch.                                               |
| `DELETE` | `/actions/{id}/what-if/{branch_id}`                                                   | Delete a what-if branch.                                               |
| `POST`   | `/types/{type_id}/properties/{property_id}/objects/{obj_id}/inline-edit`              | Single inline edit — TASK L.                                           |
| `POST`   | `/types/{type_id}/inline-edit-batch`                                                  | Bulk inline edits with per-edit validation — TASK L.                   |
| `GET`    | `/types/{type_id}/applicable-actions?selection_kind=single\|bulk`                     | Discover actions attached to an object type — TASK N.                  |
| `POST`   | `/actions/uploads`                                                                    | Register an attachment + return `attachment_rid` — TASK P.             |

### Curl examples

```bash
# Create an action type that grounds an aircraft.
curl -X POST http://localhost:50106/api/v1/ontology/actions \
  -H "authorization: Bearer $TOKEN" -H "content-type: application/json" \
  -d '{
    "name": "ground_aircraft",
    "display_name": "Ground aircraft",
    "object_type_id": "00000000-0000-0000-0000-000000000aaa",
    "operation_kind": "update_object",
    "input_schema": [
      { "name": "next_status", "property_type": "string", "required": true }
    ],
    "config": {
      "kind": "update_object",
      "property_mappings": [
        { "property_name": "status", "input_name": "next_status" }
      ]
    }
  }'

# Validate (dry-run).
curl -X POST http://localhost:50106/api/v1/ontology/actions/$ID/validate \
  -H "authorization: Bearer $TOKEN" -H "content-type: application/json" \
  -d '{ "target_object_id": "...", "parameters": { "next_status": "grounded" } }'

# Execute a single target.
curl -X POST http://localhost:50106/api/v1/ontology/actions/$ID/execute \
  -H "authorization: Bearer $TOKEN" -H "content-type: application/json" \
  -d '{ "target_object_id": "...", "parameters": { "next_status": "grounded" }, "justification": "Maintenance" }'

# Batch execute 3 targets.
curl -X POST http://localhost:50106/api/v1/ontology/actions/$ID/execute-batch \
  -H "authorization: Bearer $TOKEN" -H "content-type: application/json" \
  -d '{ "target_object_ids": ["...","...","..."], "parameters": { "next_status": "grounded" } }'

# Aggregated metrics for the last 30 days.
curl http://localhost:50106/api/v1/ontology/actions/$ID/metrics?window=30d \
  -H "authorization: Bearer $TOKEN"

# Discover actions attached to an object type.
curl "http://localhost:50106/api/v1/ontology/types/$TYPE_ID/applicable-actions?selection_kind=single" \
  -H "authorization: Bearer $TOKEN"

# Register an attachment for an `attachment` / `media_reference` parameter.
curl -X POST http://localhost:50106/api/v1/ontology/actions/uploads \
  -H "authorization: Bearer $TOKEN" -H "content-type: application/json" \
  -d '{ "filename": "report.pdf", "content_type": "application/pdf", "size_bytes": 1024 }'
```

### Failure envelopes

| HTTP | `failure_type`        | Trigger                                                                         |
| ---- | --------------------- | ------------------------------------------------------------------------------- |
| 400  | `invalid_parameter`   | Validation, missing required input, webhook writeback failure.                  |
| 401  | `authentication`      | Missing / expired Bearer token.                                                 |
| 403  | `authorization`       | Permission key denied or `authorization_policy` evaluated to false.             |
| 404  | _(none)_              | Action type or what-if branch not found.                                        |
| 409  | `conflict`            | Unique-constraint violation, duplicate inline-edit target, future revert race.  |
| 429  | `scale_limit`         | TASK M caps (object types, objects, edit bytes, list sizes, recipients).        |
| 500  | `internal`            | Database / runtime errors not classified above.                                 |

---

## Environment variables

Read by `AppConfig::from_env` (separator `__`). Defaults match the Foundry
in-cluster service map declared in `services/edge-gateway-service/src/config.rs`.

| Variable                              | Default                       | Notes                                                            |
| ------------------------------------- | ----------------------------- | ---------------------------------------------------------------- |
| `HOST`                                | `0.0.0.0`                     | Bind address.                                                    |
| `PORT`                                | `50106`                       | TCP port.                                                        |
| `DATABASE_URL`                        | _(required)_                  | Postgres DSN for `outbox.events` plus residual legacy handler queries; the service no longer runs local migrations. |
| `JWT_SECRET`                          | _(required)_                  | Shared with `enterprise-auth-service`.                           |
| `AUDIT_SERVICE_URL`                   | `http://localhost:50115`      | `audit-compliance-service` — every execution emits an event.     |
| `DATASET_SERVICE_URL`                 | `http://localhost:50079`      | Used when an action reads from datasets (TASK H).                |
| `ONTOLOGY_SERVICE_URL`                | `http://localhost:50103`      | `ontology-definition-service` — looked-up object types.          |
| `PIPELINE_SERVICE_URL`                | `http://localhost:50081`      | Triggered after writes for downstream materialisation.           |
| `AI_SERVICE_URL`                      | `http://localhost:50127`      | Function-backed actions delegate to AI when configured.          |
| `NOTIFICATION_SERVICE_URL`            | `http://localhost:50114`      | `notification-alerting-service` for action notifications.        |
| `CONNECTOR_MANAGEMENT_SERVICE_URL`    | `http://localhost:50130`      | Webhook writeback / side-effects (TASK G).                       |
| `SEARCH_EMBEDDING_PROVIDER`           | `deterministic-hash`          | Vector search provider tag.                                      |
| `NODE_RUNTIME_COMMAND`                | `node`                        | Used by function-backed actions (TASK H).                        |

`AppState` clones each URL into the kernel; pointing them at unreachable
hosts (the integration tests use `http://127.0.0.1:1`) makes the kernel
log + continue when the side-effect is best-effort, or fail fast when it
is critical (writeback webhooks).

---

## Foundry mapping

| OpenFoundry surface                      | Foundry doc                                                       |
| ---------------------------------------- | ----------------------------------------------------------------- |
| Action authoring (CRUD)                  | `docs_original_palantir_foundry/foundry-docs/.../Action types.md` |
| Submission criteria + validation         | `.../Action types/Submission criteria.md`                         |
| Webhook writeback / side-effects (G)     | `.../Action types/Use webhooks in actions.md`                     |
| Function-backed actions (H)              | `.../Action types/Function-backed actions.md`                     |
| Notifications (I)                        | `.../Action types/Notifications.md`                               |
| Struct parameters (J)                    | `.../Action types/Struct parameters.md`                           |
| Form schema overrides (K)                | `.../Action types/Form schema.md`                                 |
| Bulk inline edits (L)                    | `.../Action types/Inline edits.md`                                |
| Scale & property limits (M)              | `.../Action types/Scale and property limits.md`                   |
| Applicable actions in apps (N)           | `.../Action types/Use actions in the platform.md`                 |
| Marketplace export of action types (O)   | `.../Marketplace/Include action types in products.md`             |
| Attachments + media references (P)       | `.../Action types/Upload attachments.md`, `.../Upload media.md`   |
| Metrics + revert ledger (E/F)            | `.../Action types/Action history.md`                              |
| What-if branches                         | `.../Action types/What-if analysis.md`                            |

---

## Local development

```bash
# Run the binary against the dev compose stack.
DATABASE_URL=postgres://postgres:postgres@localhost:5432/openfoundry \
JWT_SECRET=dev-secret \
cargo run -p ontology-actions-service

# Lint + unit tests for the kernel + service.
cargo test -p ontology-kernel -p ontology-actions-service

# End-to-end coverage (requires Docker for the testcontainers Postgres).
just test-actions
```
