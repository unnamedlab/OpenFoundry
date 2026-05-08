# Quality Gates

OpenFoundry uses CI as an executable compatibility contract across Go services, frontend code, generated artifacts, infra packaging, SDK outputs, and migration/parity evidence.

## Workflow Inventory

| Workflow | Purpose | Typical Local Entry Point |
| --- | --- | --- |
| `openfoundry-go.yml` | Go module gate for backend/tooling generation, build, lint, tests, and integration paths. | `make ci`, `make build`, `make test`, `make test-integration` |
| `ci.yml` | Broader historical/parity gate that still encodes several service compatibility checks and migration-era assertions. | Use the closest current `make`, `pnpm`, `go run ./tools/of-cli`, or script command for the area being changed; verify workflow contents before relying on old Cargo examples. |
| `ci-frontend.yml` | Frontend lint, React/Vite type checks, unit tests, E2E, and production build. | `pnpm lint`, `pnpm check`, `pnpm test:unit`, `pnpm build`, `pnpm test:e2e` |
| `proto-check.yml` | Buf lint/breaking checks plus OpenAPI and SDK drift validation. | `buf lint proto`, `go run ./tools/of-cli docs validate-openapi --proto-dir proto --expected apps/web/public/generated/openapi/openfoundry.json`, and the `validate-sdk-*` commands in the API/SDK reference. |
| `helm-check.yml` / `helm-lint.yml` | Helm lint and render validation across deployment overlays. | `helm lint` / `helm template` against the touched chart or release. |
| `terraform-check.yml` | Terraform format plus module and schema validation. | `terraform fmt -check -recursive infra/terraform` |
| `sdk-smoke.yml` | Compiles and imports generated SDKs outside the main generation workflow. | `pnpm --dir apps/web exec tsc -p ../../sdks/typescript/openfoundry-sdk/tsconfig.json --noEmit`, `python3 -m compileall sdks/python/openfoundry-sdk`, and `javac --release 17` over the generated Java sources. |
| `iceberg-integration.yml` | Iceberg/PyIceberg compatibility checks. | `pytest tests/integration/pyiceberg` with required local services configured |
| `integration-foundry-pattern.yml` | Foundry-pattern integration coverage. | Area-specific scripts under `smoke/`, `benchmarks/`, and `infra/test-tools/`. |
| `chaos-smoke.yml` | Chaos and resilience smoke coverage. | Scripts and scenarios under `smoke/chaos` and `infra/test-tools/chaos` |
| `kafka-lint.yml` / `ceph-lint.yml` / `prometheus-rules.yml` | Static validation for Kafka, Ceph/topology, and Prometheus rule assets. | `python3 tools/kafka-lint/check_kraft.py`, `python3 tools/ceph-lint/check_topology.py`, and promtool-compatible local checks. |
| `security-audit.yml` | Scheduled and lockfile-triggered dependency/security audit. | Go dependency review and repository security tooling as configured by the workflow |
| `docker-publish.yml` | Builds and pushes selected service images to GHCR. | `docker build` with the touched service Dockerfile, or `infra/scripts/build-and-push-all.sh` for the bulk path. |
| `release.yml` | Generates tagged GitHub releases and changelog entries. | Git tag push flow |
| `deploy-docs.yml` | Builds VitePress docs and deploys them to GitHub Pages. | `cd docs && npm ci && npm run docs:build` |

## Makefile Backend Gate

For current Go backend changes, the root `Makefile` is the most direct local signal:

```bash
make tools          # install pinned generator/linter/formatter tools into ./bin
make gen            # regenerate protobuf and SQLC outputs
make build          # compile all Go packages
make build-services # compile service binaries into ./bin
make test           # run Go tests with race detector and coverage
make lint           # run golangci-lint
make ci             # tidy + vet + lint + test
```

Use `make test-integration` when the change touches testcontainers-backed code or integration-tagged packages and Docker is available.

## Executable Architecture Through Smoke Tests

The smoke suites are especially important because they validate feature chains rather than isolated units:

- `p2-runtime-critical-path.json` covers connection, dataset, pipeline, query, streaming, report, and geospatial runtime flows.
- `p3-semantic-governance-critical-path.json` covers ontology and governance-oriented semantics.
- `p4-developer-platform-critical-path.json` covers code repository and platform-builder flows.
- `p5-ai-ml-critical-path.json` covers provider-backed AI and ML paths.
- `p6-analytics-enterprise-critical-path.json` covers enterprise analytics and geospatial scenarios.

When you modify cross-cutting behavior, the smoke layer is often the first place that will tell you whether the overall platform contract still holds.

## What To Watch During Review

- Backend changes should normally pass the relevant `make` target before opening review.
- Contract changes can require protobuf generation, OpenAPI updates, SDK updates, and frontend updates in the same patch.
- Infra changes can break Helm, Terraform, Compose, or smoke setup even when unit tests pass.
- Service changes may need database, environment, Dockerfile, Compose, or chart updates outside the service folder itself.
- Frontend changes should account for React/Vite type checks, linting, unit tests, and Playwright coverage where behavior is visible.
- Docs changes should keep navigation, edit links, and Pages deployment in sync.
