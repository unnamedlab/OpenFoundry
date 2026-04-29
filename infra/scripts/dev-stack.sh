#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOG_DIR="${OPENFOUNDRY_LOG_DIR:-$ROOT_DIR/.openfoundry/logs}"
RUNTIME_ENV_FILE="$ROOT_DIR/.openfoundry/dev-stack.env"
REPORT_DELIVERY_ROOT="${OPENFOUNDRY_REPORT_DELIVERY_ROOT:-$ROOT_DIR/.openfoundry/report-delivery}"
BUILD_LOG="$LOG_DIR/cargo-build.log"
WEB_LOG="$LOG_DIR/web.log"
SERVICES=(
  identity-federation-service
  audit-compliance-service
  connector-management-service
  data-asset-catalog-service
  sql-bi-gateway-service
  pipeline-authoring-service
  ontology-definition-service
  model-catalog-service
  agent-runtime-service
  workflow-automation-service
  notebook-runtime-service
  document-reporting-service
  app-builder-service
  marketplace-service
  nexus-service
  notification-alerting-service
  gateway
)
PIDS=()

port_is_busy() {
  local port="$1"
  lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1
}

find_available_port() {
  local candidate="$1"
  while port_is_busy "$candidate"; do
    candidate=$((candidate + 1))
  done
  echo "$candidate"
}

ensure_host_port() {
  local var_name="$1"
  local preferred_port="$2"
  local fallback_port="$3"
  local label="$4"
  local configured_port="${!var_name:-}"
  local selected_port=""

  if [[ -n "$configured_port" ]]; then
    export "$var_name=$configured_port"
    return
  fi

  selected_port="$preferred_port"
  if port_is_busy "$preferred_port"; then
    selected_port="$(find_available_port "$fallback_port")"
    echo "$label host port $preferred_port is already in use; using $selected_port instead."
  fi

  export "$var_name=$selected_port"
}

replace_local_port() {
  local value="$1"
  local default_port="$2"
  local resolved_port="$3"

  value="${value//localhost:$default_port/localhost:$resolved_port}"
  value="${value//127.0.0.1:$default_port/127.0.0.1:$resolved_port}"
  echo "$value"
}

configure_local_infra_ports() {
  ensure_host_port OPENFOUNDRY_POSTGRES_HOST_PORT 5432 55432 "PostgreSQL"
  ensure_host_port OPENFOUNDRY_REDIS_HOST_PORT 6379 56379 "Redis"
  ensure_host_port OPENFOUNDRY_NATS_HOST_PORT 4222 54222 "NATS"
  ensure_host_port OPENFOUNDRY_NATS_MONITOR_HOST_PORT 8222 58222 "NATS monitor"
  ensure_host_port OPENFOUNDRY_MINIO_API_HOST_PORT 9000 59000 "MinIO API"
  ensure_host_port OPENFOUNDRY_MINIO_CONSOLE_HOST_PORT 9001 59001 "MinIO console"
  ensure_host_port OPENFOUNDRY_MEILISEARCH_HOST_PORT 7700 57700 "Meilisearch"
}

rewrite_local_endpoints() {
  DATABASE_URL="$(replace_local_port "${DATABASE_URL:-postgres://openfoundry:openfoundry@localhost:5432/openfoundry}" 5432 "$OPENFOUNDRY_POSTGRES_HOST_PORT")"
  REDIS_URL="$(replace_local_port "${REDIS_URL:-redis://localhost:6379}" 6379 "$OPENFOUNDRY_REDIS_HOST_PORT")"
  NATS_URL="$(replace_local_port "${NATS_URL:-nats://localhost:4222}" 4222 "$OPENFOUNDRY_NATS_HOST_PORT")"
  S3_ENDPOINT="$(replace_local_port "${S3_ENDPOINT:-http://localhost:9000}" 9000 "$OPENFOUNDRY_MINIO_API_HOST_PORT")"
  MEILISEARCH_URL="$(replace_local_port "${MEILISEARCH_URL:-http://localhost:7700}" 7700 "$OPENFOUNDRY_MEILISEARCH_HOST_PORT")"
  # Qdrant se retira por restricción de licencia OSS; sustituto futuro: Vespa
  # (Apache-2.0). Por ahora pgvector cubre el caso embebido y reutiliza
  # DATABASE_URL.

  export DATABASE_URL
  export REDIS_URL
  export NATS_URL
  export S3_ENDPOINT
  export MEILISEARCH_URL
}

service_database_name() {
  local service_name="$1"
  echo "openfoundry_${service_name//-/_}"
}

service_database_url() {
  local service_name="$1"
  local database_name
  local base_url="$DATABASE_URL"
  local query_suffix=""

  database_name="$(service_database_name "$service_name")"
  if [[ "$base_url" == *\?* ]]; then
    query_suffix="?${base_url#*\?}"
    base_url="${base_url%%\?*}"
  fi

  echo "${base_url%/*}/$database_name$query_suffix"
}

provision_service_databases() {
  local service_name=""
  local database_name=""
  local exists=""

  echo "Provisioning per-service PostgreSQL databases..."
  for service_name in "${SERVICES[@]}"; do
    if [[ "$service_name" == "gateway" ]]; then
      continue
    fi

    database_name="$(service_database_name "$service_name")"
    exists="$(
      cd "$ROOT_DIR"
      docker compose -p "$OPENFOUNDRY_DOCKER_PROJECT_NAME" -f infra/docker-compose.yml -f infra/docker-compose.dev.yml exec -T postgres \
        psql -U openfoundry -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$database_name'"
    )"

    if [[ "$exists" != "1" ]]; then
      (
        cd "$ROOT_DIR"
        docker compose -p "$OPENFOUNDRY_DOCKER_PROJECT_NAME" -f infra/docker-compose.yml -f infra/docker-compose.dev.yml exec -T postgres \
          createdb -U openfoundry "$database_name"
      ) >/dev/null
    fi
  done
}

prepare_local_runtime_dirs() {
  mkdir -p "$LOG_DIR"
  mkdir -p "$(dirname "$RUNTIME_ENV_FILE")"
  mkdir -p "$REPORT_DELIVERY_ROOT"
}

require_command() {
  local command_name="$1"
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "Missing required command: $command_name" >&2
    exit 1
  fi
}

load_env() {
  local env_file=""

  if [[ -f "$RUNTIME_ENV_FILE" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "$RUNTIME_ENV_FILE"
    set +a
  fi

  if [[ -f "$ROOT_DIR/.env" ]]; then
    env_file="$ROOT_DIR/.env"
  elif [[ -f "$ROOT_DIR/.env.example" ]]; then
    env_file="$ROOT_DIR/.env.example"
    echo "Using .env.example defaults. Create .env if you want to override credentials or ports."
  fi

  if [[ -n "$env_file" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "$env_file"
    set +a
  fi

  export OPENFOUNDRY_ENV="${OPENFOUNDRY_ENV:-development}"
  export OPENFOUNDRY_DOCKER_PROJECT_NAME="${OPENFOUNDRY_DOCKER_PROJECT_NAME:-openfoundry-dev}"

  if [[ "${OPENFOUNDRY_RUNTIME_PROJECT_NAME:-}" != "$OPENFOUNDRY_DOCKER_PROJECT_NAME" ]]; then
    unset OPENFOUNDRY_POSTGRES_HOST_PORT
    unset OPENFOUNDRY_REDIS_HOST_PORT
    unset OPENFOUNDRY_NATS_HOST_PORT
    unset OPENFOUNDRY_NATS_MONITOR_HOST_PORT
    unset OPENFOUNDRY_MINIO_API_HOST_PORT
    unset OPENFOUNDRY_MINIO_CONSOLE_HOST_PORT
    unset OPENFOUNDRY_MEILISEARCH_HOST_PORT
  fi
}

persist_runtime_env() {
  cat >"$RUNTIME_ENV_FILE" <<EOF
OPENFOUNDRY_RUNTIME_PROJECT_NAME=$OPENFOUNDRY_DOCKER_PROJECT_NAME
OPENFOUNDRY_POSTGRES_HOST_PORT=$OPENFOUNDRY_POSTGRES_HOST_PORT
OPENFOUNDRY_REDIS_HOST_PORT=$OPENFOUNDRY_REDIS_HOST_PORT
OPENFOUNDRY_NATS_HOST_PORT=$OPENFOUNDRY_NATS_HOST_PORT
OPENFOUNDRY_NATS_MONITOR_HOST_PORT=$OPENFOUNDRY_NATS_MONITOR_HOST_PORT
OPENFOUNDRY_MINIO_API_HOST_PORT=$OPENFOUNDRY_MINIO_API_HOST_PORT
OPENFOUNDRY_MINIO_CONSOLE_HOST_PORT=$OPENFOUNDRY_MINIO_CONSOLE_HOST_PORT
OPENFOUNDRY_MEILISEARCH_HOST_PORT=$OPENFOUNDRY_MEILISEARCH_HOST_PORT
EOF
}

start_infra() {
  if [[ "${OPENFOUNDRY_SKIP_INFRA:-0}" == "1" ]]; then
    echo "Skipping docker compose because OPENFOUNDRY_SKIP_INFRA=1"
    return
  fi

  echo "Starting local infrastructure with docker compose..."
  (
    cd "$ROOT_DIR"
    docker compose -p "$OPENFOUNDRY_DOCKER_PROJECT_NAME" -f infra/docker-compose.yml -f infra/docker-compose.dev.yml up -d
  )
}

build_workspace() {
  if [[ "${OPENFOUNDRY_SKIP_BUILD:-0}" == "1" ]]; then
    echo "Skipping cargo build because OPENFOUNDRY_SKIP_BUILD=1"
    return
  fi

  echo "Building Rust workspace once before launch..."
  if ! (
    cd "$ROOT_DIR"
    cargo build --workspace
  ) >"$BUILD_LOG" 2>&1; then
    echo "Workspace build failed. Check $BUILD_LOG" >&2
    tail -n 40 "$BUILD_LOG" >&2 || true
    exit 1
  fi
}

start_service() {
  local service_name="$1"
  local binary_path="$ROOT_DIR/target/debug/$service_name"
  local log_file="$LOG_DIR/$service_name.log"
  local database_url=""

  if [[ ! -x "$binary_path" ]]; then
    echo "Binary not found for $service_name at $binary_path" >&2
    echo "Try rerunning without OPENFOUNDRY_SKIP_BUILD=1." >&2
    exit 1
  fi

  echo "Starting $service_name..."
  if [[ "$service_name" != "gateway" ]]; then
    database_url="$(service_database_url "$service_name")"
  fi

  (
    cd "$ROOT_DIR"
    if [[ "$service_name" == "document-reporting-service" ]]; then
      DATABASE_URL="$database_url" LOCAL_DELIVERY_ROOT="$REPORT_DELIVERY_ROOT" "$binary_path"
    elif [[ -n "$database_url" ]]; then
      DATABASE_URL="$database_url" "$binary_path"
    else
      "$binary_path"
    fi
  ) >"$log_file" 2>&1 &
  PIDS+=("$!")
}

start_frontend() {
  if [[ ! -d "$ROOT_DIR/apps/web/node_modules" ]]; then
    echo "Frontend dependencies are missing. Run 'pnpm install' first." >&2
    exit 1
  fi

  echo "Starting web frontend..."
  (
    cd "$ROOT_DIR"
    pnpm --filter @open-foundry/web dev
  ) >"$WEB_LOG" 2>&1 &
  PIDS+=("$!")
}

wait_for_http() {
  local url="$1"
  local label="$2"
  local attempts="${3:-90}"

  for ((i = 1; i <= attempts; i++)); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      echo "$label is ready at $url"
      return 0
    fi
    sleep 1
  done

  echo "Timed out waiting for $label at $url" >&2
  return 1
}

cleanup() {
  local pid
  for pid in "${PIDS[@]}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
  wait >/dev/null 2>&1 || true
}

monitor_processes() {
  local pid
  while true; do
    for pid in "${PIDS[@]}"; do
      if ! kill -0 "$pid" >/dev/null 2>&1; then
        echo "A local process exited unexpectedly. Logs are in $LOG_DIR" >&2
        cleanup
        exit 1
      fi
    done
    sleep 2
  done
}

main() {
  prepare_local_runtime_dirs

  require_command docker
  require_command cargo
  require_command pnpm
  require_command curl
  require_command lsof

  load_env
  configure_local_infra_ports
  rewrite_local_endpoints
  persist_runtime_env
  start_infra
  provision_service_databases
  build_workspace

  trap cleanup EXIT INT TERM

  for service_name in "${SERVICES[@]}"; do
    start_service "$service_name"
    sleep 0.2
  done

  start_frontend

  wait_for_http "http://127.0.0.1:${GATEWAY_PORT:-8080}/health" "Gateway"
  wait_for_http "http://127.0.0.1:5173" "Web app"

  cat <<EOF

OpenFoundry is running locally.

Web UI:      http://127.0.0.1:5173
API Gateway: http://127.0.0.1:${GATEWAY_PORT:-8080}
Logs:        $LOG_DIR
Compose:     $OPENFOUNDRY_DOCKER_PROJECT_NAME
Postgres:    localhost:${OPENFOUNDRY_POSTGRES_HOST_PORT}
Redis:       localhost:${OPENFOUNDRY_REDIS_HOST_PORT}
NATS:        localhost:${OPENFOUNDRY_NATS_HOST_PORT}
MinIO API:   http://localhost:${OPENFOUNDRY_MINIO_API_HOST_PORT}
MinIO UI:    http://localhost:${OPENFOUNDRY_MINIO_CONSOLE_HOST_PORT}
Meilisearch: http://localhost:${OPENFOUNDRY_MEILISEARCH_HOST_PORT}

Press Ctrl+C to stop the locally started services.
The docker infrastructure stays up; stop it with:
  just infra-down
EOF

  monitor_processes
}

main "$@"
