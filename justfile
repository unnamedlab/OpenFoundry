# OpenFoundry — Task Runner
# Usage: just <recipe>

set dotenv-load := true

# ── Default ──────────────────────────────────────────────────

default:
    @just --list

# ── Build ────────────────────────────────────────────────────

# Build all Rust services and libraries
build:
    cargo build --workspace

# Build in release mode
build-release:
    cargo build --workspace --release

# Build a specific service
build-svc svc:
    cargo build -p {{svc}}

# ── Test ─────────────────────────────────────────────────────

# Run all tests
test:
    cargo test --workspace

# Run tests for a specific crate
test-svc svc:
    cargo test -p {{svc}}

# Run tests with output
test-verbose:
    cargo test --workspace -- --nocapture

# TASK Q — Action types end-to-end coverage. Runs the Rust integration
# suite under `libs/ontology-kernel/tests/actions_integration.rs` (boots an
# ephemeral Postgres via testcontainers — Docker required) plus the
# Playwright spec at `apps/web/tests/e2e/action-types.spec.ts`.
test-actions:
    cargo test -p ontology-kernel --features it --test actions_integration --test actions_scale -- --include-ignored
    cd apps/web && pnpm exec playwright test tests/e2e/action-types.spec.ts

# Run every Cassandra-backed integration test across the workspace.
# Crates opt in by exposing an `it-cassandra` feature that turns on
# `testing/it-cassandra` (see `libs/testing/Cargo.toml`). Requires
# Docker; each test boots an ephemeral `cassandra:5.0` container.
test-cassandra:
    cargo test --workspace --features testing/it-cassandra -- --include-ignored

# Run every Temporal-backed integration test across the workspace.
# Crates opt in by exposing an `it-temporal` feature that turns on
# `testing/it-temporal`. Requires Docker; each test boots an
# ephemeral `temporalio/auto-setup:1.24` container.
test-temporal:
    cargo test --workspace --features testing/it-temporal -- --include-ignored

# Boot a one-shot Cassandra 5 node on localhost:9042 for ad-hoc dev
# work (cqlsh sessions, schema scratching). Foreground; Ctrl-C to
# tear down. For a real multi-node dev cluster use the k8s manifests
# under `infra/k8s/platform/manifests/cassandra/`.
dev-up-cassandra:
    docker run --rm -it --name of-cass-dev \
        -p 9042:9042 \
        -e CASSANDRA_CLUSTER_NAME=of-dev \
        -e CASSANDRA_DC=dc1 \
        -e CASSANDRA_RACK=rack1 \
        -e CASSANDRA_ENDPOINT_SNITCH=GossipingPropertyFileSnitch \
        -e HEAP_NEWSIZE=128M \
        -e MAX_HEAP_SIZE=512M \
        cassandra:5.0
    cd apps/web && pnpm exec playwright test tests/e2e/action-types.spec.ts

# ── Lint & Format ────────────────────────────────────────────

# Run all lints
lint: fmt-check clippy

# Check formatting
fmt-check:
    cargo fmt --all -- --check

# Format all code
fmt:
    cargo fmt --all

# Run Clippy
clippy:
    cargo clippy --workspace --all-targets -- -D warnings

# Run cargo-deny (license & vulnerability audit)
deny:
    cargo deny check

# ── Run ──────────────────────────────────────────────────────

# Run a specific service
run svc:
    cargo run -p {{svc}}

# Run the gateway
run-gateway:
    cargo run -p edge-gateway-service

# Run the OpenFoundry CLI
of args='':
    cargo run -p of-cli -- {{args}}

# ── Go workers (Temporal) ────────────────────────────────────

# Build every Go worker under workers-go/. Uses the workspace go.work.
go-build:
    cd workers-go && go build ./...

# Run go test across every module in workers-go/.
go-test:
    cd workers-go && go test ./...

# Tidy go.sum across every module in workers-go/.
go-tidy:
    cd workers-go/workflow-automation && go mod tidy
    cd workers-go/approvals && go mod tidy
    cd workers-go/automation-ops && go mod tidy

# Run a single Temporal worker locally (foreground; Ctrl-C to stop).
# Expects a running Temporal frontend on TEMPORAL_HOST_PORT
# (default 127.0.0.1:7233 — boot one with
# `docker run --rm -p 7233:7233 -p 8233:8233 temporalio/auto-setup:1.24`).
go-worker name:
    cd workers-go/{{name}} && go run .

# ── Database ─────────────────────────────────────────────────

# Run all migrations
db-migrate:
    @for dir in services/*/migrations; do \
        svc=$(basename $(dirname "$dir")); \
        echo "→ Migrating $svc..."; \
        cargo sqlx migrate run --source "$dir"; \
    done

# Create a new migration for a service
db-new-migration svc name:
    cargo sqlx migrate add -r --source "services/{{svc}}/migrations" {{name}}

# ── Protobuf ─────────────────────────────────────────────────

# Generate code from .proto files
proto-gen:
    cd proto && buf generate

# Generate OpenAPI docs from proto services
openapi-gen:
    cargo run -p of-cli -- docs generate-openapi --output apps/web/static/generated/openapi/openfoundry.json

# Validate checked-in OpenAPI docs against the current proto/tooling state
openapi-check:
    cargo run -p of-cli -- docs validate-openapi --input apps/web/static/generated/openapi/openfoundry.json

# Generate the official TypeScript SDK from the checked-in OpenAPI contract
sdk-typescript-gen:
    cargo run -p of-cli -- docs generate-sdk-typescript --input apps/web/static/generated/openapi/openfoundry.json --output sdks/typescript/openfoundry-sdk

# Validate checked-in SDK output against the current OpenAPI contract
sdk-typescript-check:
    cargo run -p of-cli -- docs validate-sdk-typescript --input apps/web/static/generated/openapi/openfoundry.json --output sdks/typescript/openfoundry-sdk

# Typecheck the generated SDK with the existing frontend TypeScript toolchain
sdk-typescript-typecheck:
    cd apps/web && pnpm exec tsc -p ../../sdks/typescript/openfoundry-sdk/tsconfig.json --noEmit

# Generate the official Python SDK from the checked-in OpenAPI contract
sdk-python-gen:
    cargo run -p of-cli -- docs generate-sdk-python --input apps/web/static/generated/openapi/openfoundry.json --output sdks/python/openfoundry-sdk

# Validate checked-in Python SDK output against the current OpenAPI contract
sdk-python-check:
    cargo run -p of-cli -- docs validate-sdk-python --input apps/web/static/generated/openapi/openfoundry.json --output sdks/python/openfoundry-sdk

# Compile the generated Python SDK to catch syntax/import issues
sdk-python-compile:
    python3 -m compileall sdks/python/openfoundry-sdk

# Generate the official Java SDK from the checked-in OpenAPI contract
sdk-java-gen:
    cargo run -p of-cli -- docs generate-sdk-java --input apps/web/static/generated/openapi/openfoundry.json --output sdks/java/openfoundry-sdk

# Validate checked-in Java SDK output against the current OpenAPI contract
sdk-java-check:
    cargo run -p of-cli -- docs validate-sdk-java --input apps/web/static/generated/openapi/openfoundry.json --output sdks/java/openfoundry-sdk

# Compile the generated Java SDK sources (requires JDK 17+)
sdk-java-compile:
    find sdks/java/openfoundry-sdk/src/main/java -name '*.java' -print0 | xargs -0 javac --release 17

# Validate the Strimzi Kafka cluster manifest against the KRaft contract
kafka-kraft-lint:
    python3 tools/kafka-lint/check_kraft.py

# Validate the Rook-Ceph manifests against the quorum / rack-awareness contract
ceph-topology-lint:
    python3 tools/ceph-lint/check_topology.py

# Validate platform and app Helm releases across dev/staging/prod profiles via
# helmfile. Renders manifests for each environment to /tmp/openfoundry-<env>.yaml
# so they can be diffed in CI. Requires `helmfile` 0.165+ and `helm` 3.14+.
helm-check:
    cd infra/k8s/helm && for release in of-platform of-data-engine of-ontology of-ml-aip of-apps-ops; do \
        helm dependency update $$release; \
    done
    cd infra/k8s/platform && helmfile -e dev     lint
    cd infra/k8s/platform && helmfile -e staging lint
    cd infra/k8s/platform && helmfile -e prod    lint
    cd infra/k8s/platform && helmfile -e prod template --args "--api-versions monitoring.coreos.com/v1/PodMonitor" > /tmp/openfoundry-platform-prod.yaml
    cd infra/k8s/helm && helmfile -e dev     lint
    cd infra/k8s/helm && helmfile -e staging lint
    cd infra/k8s/helm && helmfile -e prod    lint
    cd infra/k8s/helm && helmfile -e dev     template > /tmp/openfoundry-dev.yaml
    cd infra/k8s/helm && helmfile -e staging template > /tmp/openfoundry-staging.yaml
    cd infra/k8s/helm && helmfile -e prod    template > /tmp/openfoundry-prod.yaml

# Apply platform releases for the given environment via helmfile. Run this
# before `helm-deploy` for a fresh cluster.
platform-deploy env='dev':
    cd infra/k8s/platform && helmfile -e {{env}} apply

# Show platform changes for the given environment (helm diff plugin required).
platform-diff env='dev':
    cd infra/k8s/platform && helmfile -e {{env}} diff

# Apply the five split Helm releases for the given environment via helmfile.
# Examples:
#   just helm-deploy dev
#   just helm-deploy prod
#   just helm-deploy airgap        # prod profile + airgap posture
#   just helm-deploy sovereign-eu  # prod profile + EU residency posture
helm-deploy env='dev':
    cd infra/k8s/helm && helmfile -e {{env}} apply

# Show what would change for the given environment (helm diff plugin required).
helm-diff env='dev':
    cd infra/k8s/helm && helmfile -e {{env}} diff

# Tear down the five releases for the given environment. Destructive.
helm-destroy env='dev':
    cd infra/k8s/helm && helmfile -e {{env}} destroy

# Generate Terraform provider schema for docs and portal consumption
terraform-schema:
    cargo run -p of-cli -- terraform schema --output infra/terraform/providers/openfoundry/provider.schema.json
    cargo run -p of-cli -- terraform schema --output apps/web/static/generated/terraform/openfoundry-provider.json

# Run reproducible benchmark suite against a live stack
bench-critical-paths:
    cargo run -p of-cli -- bench run --scenario benchmarks/scenarios/critical-paths.json --output benchmarks/results/critical-paths.json

# Run the ontology hot-path mixed workload (S1.8) against a live stack via k6.
# Requires k6 1.0+ and OF_BENCH_* env vars (see benchmarks/ontology/README.md).
bench-ontology:
    benchmarks/ontology/scripts/run-s1-baseline.sh

# Sequential latency baseline for the ontology hot path (sin RPS shape).
bench-ontology-baseline:
    cargo run -p of-cli -- bench run \
        --scenario benchmarks/ontology/scenarios/ontology-mix.json \
        --output benchmarks/results/ontology-mix-baseline.json

# Capture a Cassandra JMX baseline (nodetool tablestats -F json per
# node × keyspace) into benchmarks/results/cassandra-baseline-<UTC>/
# for ADR-0012 §A.3. Idempotent: re-running suffixes -2/-3/... so an
# existing capture is never overwritten. Override defaults via env
# vars OF_BENCH_CASS_NS / OF_BENCH_CASS_POD_LABEL / OF_BENCH_CASS_CONTAINER
# (see the script header for details).
bench-capture-baseline:
    bash benchmarks/ontology/scripts/capture-cassandra-baseline.sh

# Run the critical-path smoke suite against a live stack
smoke-critical-paths:
    cargo run -p of-cli -- smoke run --scenario smoke/scenarios/p2-runtime-critical-path.json --output smoke/results/p2-runtime-critical-path.json

# Run the semantic/governance smoke suite against a live stack
smoke-p3-semantic-governance:
    cargo run -p of-cli -- smoke run --scenario smoke/scenarios/p3-semantic-governance-critical-path.json --output smoke/results/p3-semantic-governance-critical-path.json

# Run the developer platform smoke suite against a live stack
smoke-p4-developer-platform:
    cargo run -p of-cli -- smoke run --scenario smoke/scenarios/p4-developer-platform-critical-path.json --output smoke/results/p4-developer-platform-critical-path.json

# Run the AI/ML smoke suite against a live stack
smoke-p5-ai-ml:
    cargo run -p of-cli -- smoke run --scenario smoke/scenarios/p5-ai-ml-critical-path.json --output smoke/results/p5-ai-ml-critical-path.json

# Run the analytics/control-panel/nexus/enterprise smoke suite against a live stack
smoke-p6-analytics-enterprise:
    cargo run -p of-cli -- smoke run --scenario smoke/scenarios/p6-analytics-enterprise-critical-path.json --output smoke/results/p6-analytics-enterprise-critical-path.json

# Lint proto files
proto-lint:
    cd proto && buf lint

# Check for breaking changes
proto-breaking:
    cd proto && buf breaking --against '.git#branch=main'

# ── Docker ───────────────────────────────────────────────────

# Start dev infrastructure (Postgres, Redis, NATS, MinIO, Vespa Lite). Add
# `--profile demo` to also bring up the optional Meilisearch (see ADR-0007).
infra-up:
    docker compose -p "${OPENFOUNDRY_DOCKER_PROJECT_NAME:-openfoundry-dev}" -f infra/docker-compose.yml -f infra/docker-compose.dev.yml up -d

# Start dev infrastructure plus containerized auth/gateway/web stack
app-up:
    docker compose -p "${OPENFOUNDRY_DOCKER_PROJECT_NAME:-openfoundry-dev}" -f infra/docker-compose.yml -f infra/docker-compose.dev.yml --profile app up -d

# Build one cumulative rollout wave with bounded parallelism.
# Waves: foundation, data, knowledge, intelligence, experience, edge
stack-build wave parallel='4':
    docker compose -p "${OPENFOUNDRY_DOCKER_PROJECT_NAME:-openfoundry-dev}" -f compose.yaml --profile {{wave}} build --parallel {{parallel}}

# Start one cumulative rollout wave from the repo root compose stack.
# Example: just stack-up foundation
stack-up wave:
    docker compose -p "${OPENFOUNDRY_DOCKER_PROJECT_NAME:-openfoundry-dev}" -f compose.yaml --profile {{wave}} up -d

# Stop dev infrastructure
infra-down:
    docker compose -p "${OPENFOUNDRY_DOCKER_PROJECT_NAME:-openfoundry-dev}" -f infra/docker-compose.yml -f infra/docker-compose.dev.yml down

# NOTE: `infra-up-full` (dev stack + monitoring) was removed because the
# `infra/docker-compose.monitoring.yml` stub was empty and gave a false signal
# of an existing monitoring stack. The recipe will be reintroduced as part of
# the formal observability work (T17). See docs/observability/index.md.

# Build all Docker images
docker-build:
    @for dir in services/*/Dockerfile; do \
        svc=$(basename $(dirname "$dir")); \
        echo "→ Building $svc..."; \
        docker build -t "open-foundry/$svc:latest" -f "$dir" .; \
    done

# ── Frontend ─────────────────────────────────────────────────

# Install frontend dependencies
fe-install:
    cd apps/web && pnpm install

# Run frontend dev server
fe-dev:
    cd apps/web && pnpm dev

# Install documentation website dependencies
docs-install:
    cd docs && npm ci

# Run documentation website locally
docs-dev:
    cd docs && npm run docs:dev

# Build documentation website
docs-build:
    cd docs && npm run docs:build

# Preview built documentation website
docs-preview:
    cd docs && npm run docs:preview

# Start infra, backend services, and frontend together for manual local verification
dev-stack:
    ./infra/scripts/dev-stack.sh

# Faster restart path when infra is already running and Rust binaries are already built
dev-stack-fast:
    OPENFOUNDRY_SKIP_INFRA=1 OPENFOUNDRY_SKIP_BUILD=1 ./infra/scripts/dev-stack.sh

# Smoke-test gateway, auth, datasets, and ontology against a running local stack
smoke:
    ./infra/scripts/smoke.sh

# Build frontend
fe-build:
    cd apps/web && pnpm build

# Lint frontend
fe-lint:
    cd apps/web && pnpm lint

# Typecheck frontend
fe-check:
    cd apps/web && pnpm check

# Run frontend tests
fe-test:
    cd apps/web && pnpm test

# Run frontend unit tests
fe-test-unit:
    cd apps/web && pnpm test:unit

# Run frontend E2E tests
fe-test-e2e:
    cd apps/web && pnpm test:e2e

# Run frontend CI checks locally
ci-frontend: fe-lint fe-check fe-test-unit fe-build
    @echo "✅ Frontend CI checks passed"

# ── CI ───────────────────────────────────────────────────────

# Run full CI checks locally
ci: lint test proto-lint openapi-check sdk-typescript-check sdk-typescript-typecheck sdk-python-check sdk-python-compile ci-frontend
    @echo "✅ All CI checks passed"

# ── Cleanup ──────────────────────────────────────────────────

# Clean build artifacts
clean:
    cargo clean
    rm -rf apps/web/node_modules apps/web/.svelte-kit apps/web/build
