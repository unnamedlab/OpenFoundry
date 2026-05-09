# CLAUDE.md — libs/ontology-kernel

This is the **largest shared library** in the repo (~35k LOC across
domain, models, handlers, stores). Read this before opening files —
loading the wrong subpackage will burn 10–15k tokens for nothing.

## Where to look first

| You want to change… | Open this |
|---|---|
| HTTP route shape / payload | `handlers/<area>/<area>.go` |
| Pure logic / invariants | `domain/<topic>.go` |
| DB access trait | `stores/stores.go` (interface) + `stores/pg.go` (impl) |
| Wire types | `models/<entity>.go` |
| Service-wide state | `appstate.go` |

The 16 `handlers/<area>/` subdirectories are the bounded contexts:
`actions`, `bindings`, `funnel`, `functions`, `interfaces`, `links`,
`objects`, `objectsets`, `projects`, `properties`, `rules`, `search`,
`sharedproperties`, `storage`, `types`. **Touch only the area you need.**

## Files to handle with care (size warning)

These exceed any reasonable attention window. Don't load them in full
unless absolutely necessary; navigate by symbol with `grep -n`:

| File | Lines | What it owns |
|---|---:|---|
| `handlers/actions/execute.go` | 2230 | Action execution pipeline |
| `handlers/projects/projects.go` | 1440 | Project CRUD |
| `domain/rules.go` | 1334 | Rule evaluation |
| `domain/function_runtime.go` | 1091 | Function exec (pyo3 sidecar) |
| `handlers/funnel/funnel.go` | 1006 | Funnel queries |
| `handlers/actions/side_effects.go` | 893 | Action effects |
| `handlers/bindings/bindings.go` | 889 | Type bindings |
| `handlers/functions/functions.go` | 857 | Function CRUD |
| `stores/mock.go` | 849 | In-memory store (tests) |

If you're modifying one of these, add a focused test next to the
specific function rather than reading the whole file. Splitting these is
on the long-term roadmap but **not** part of any feature task — don't
get sidetracked by a refactor.

## Architecture

`AppState` (`appstate.go`) is the dependency container shared by every
handler in this kernel. Two consumers wire it:

- `services/ontology-actions-service/`
- `services/ontology-exploratory-analysis-service/`

Each handler is a free function or method that takes `*AppState` (or a
narrower interface) and returns `http.HandlerFunc`. Don't add
package-level globals; thread state through `AppState`.

`AppState.DB` is a raw `*pgxpool.Pool` (legacy direct-PG paths).
`AppState.Stores` is the typed interface set (`stores.Stores`) — prefer
this for new code. Both fields coexist by design during the
Cassandra-Foundry parity migration.

## Domain ⇄ handlers contract

- `domain/` is **pure** Go: no `http.*`, no JSON encoding, no Postgres
  connections. Functions return values + sentinel errors.
- `handlers/` does HTTP-isms only: parse request, call domain, encode
  response, map errors.
- Crossing those layers (e.g. domain returning `http.StatusNotFound`)
  is a smell — fix it.

## Wire compatibility invariant

Every JSON shape, error string, enum token, and ordering rule has a
golden test (look for `*_test.go` with `golden` or `fixture` in the
name, plus `models/contract_fixtures_test.go`). **If a test pins a
field name, do not rename it casually** — there are external consumers
(SDKs, archived Rust binaries during migration, frontend types).

## Testing this lib

```sh
# Fast unit tests:
go test ./libs/ontology-kernel/...

# Integration (Postgres testcontainer, needs Docker):
go test -tags integration ./libs/ontology-kernel/...
```

Heavy fixtures live in `models/testdata/`.

## Don't read

- `docs/archive/INVENTORY-PHASE6.md` — phase tracker, superseded.
- `ONTOLOGY-KERNEL-MIGRATION.md` (root) — migration log only.
- The Rust references in `doc.go` files — they're historical, the Rust
  source is no longer in this tree.
