# federation-product-exchange-service

## LLM context

Owns marketplace/product distribution, products, listings, installs, dependency planning, peers, contracts, queries, and shares.

Agent note: bridges marketplace bundles with ontology, actions, pipelines, and application-composition services.

## Entrypoints

- `cmd/federation-product-exchange-service/main.go` builds the `federation-product-exchange-service` binary.

## Current HTTP / runtime surface

- `/api/v1/marketplace/products*`
- `/api/v1/marketplace/listings*`
- `/api/v1/marketplace/installs*`
- `/api/v1/product-distribution/*`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- Contains `10` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `marketplace`, `models`, `observability`, `productdistribution`, `products`, `repo`, `server`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `APPLICATION_COMPOSITION_URL`, `DATABASE_URL`, `HOST`, `JWT_SECRET`, `MARKETPLACE_BUNDLE_ROOT`, `MARKETPLACE_DATABASE_URL`, `MARKETPLACE_SIGN_KEY`, `ONTOLOGY_ACTIONS_URL`
- `ONTOLOGY_DEFINITION_URL`, `PIPELINE_BUILD_URL`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/federation-product-exchange-service ./services/federation-product-exchange-service/cmd/federation-product-exchange-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
