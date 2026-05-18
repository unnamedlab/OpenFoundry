# Service skeleton

Reference layout for every Go service in `openfoundry-go/services/`.
This tree lives under `docs/templates/service-skeleton/` as **textual
reference only**: every `.go` file carries a `//go:build ignore`
header so `go build ./...`, `go test ./...` and `make build-services`
skip it in place. The `Dockerfile`, `config.yaml`, and the
`services/template/...` paths inside `cmd/template/main.go` describe
the post-copy state.

To use it:

1. `cp -r docs/templates/service-skeleton services/<your-service>`
2. Drop the `//go:build ignore` headers on the copied `.go` files —
   `find services/<your-service> -name '*.go' -exec sed -i '' '/^\/\/go:build ignore$/,/^$/d' {} \;`
3. Replace every `template` occurrence with `<your-service>` (paths,
   import roots, the `cmd/template/` directory, the `service.name`
   field in `config.yaml`, the Dockerfile `SERVICE_NAME` defaults and
   labels).
4. Register the service in the Helm chart, ArgoCD app set and (if it
   takes external traffic) the edge gateway router table — see
   [`CONTRIBUTING.md`](../../../CONTRIBUTING.md) §"Adding a new
   service".

The resulting binary exposes:

- `GET /healthz` — liveness payload from `libs/core-models/health`
- `GET /metrics` — Prometheus scrape endpoint
- bearer-token authentication via `auth-middleware` on the `/api/*` mount

## Layout

```
services/<name>/
├── cmd/<name>/main.go         # entrypoint
├── internal/
│   ├── config/config.go        # koanf-backed config
│   ├── server/server.go        # router + server lifecycle
│   ├── handler/                # HTTP handlers
│   ├── service/                # business logic
│   └── repo/                   # data access (sqlc-generated)
├── api/openapi.yaml            # OpenAPI contract (optional)
├── config.yaml                 # default config baked into the image
├── Dockerfile
└── README.md
```

## Build

```sh
go build -o bin/<name> ./services/<name>/cmd/<name>
```

Or via the root Makefile:

```sh
make build-services
```

## Image

```sh
docker build -t openfoundry/<name>:dev -f services/<name>/Dockerfile .
```

The Dockerfile is a multi-stage build that produces a distroless static
image (~25 MB) — no shell, no package manager, runs as nonroot.
