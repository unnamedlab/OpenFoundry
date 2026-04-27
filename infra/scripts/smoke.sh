#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUNTIME_ENV_FILE="$ROOT_DIR/.openfoundry/dev-stack.env"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/openfoundry-smoke.XXXXXX")"

REQUEST_STATUS=""
REQUEST_RESPONSE_FILE=""

cleanup() {
  rm -rf "$TMP_DIR"
}

trap cleanup EXIT

require_command() {
  local command_name="$1"
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "Missing required command: $command_name" >&2
    exit 1
  fi
}

normalize_host() {
  local host="$1"
  if [[ "$host" == "0.0.0.0" ]]; then
    echo "127.0.0.1"
  else
    echo "$host"
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
  fi

  if [[ -n "$env_file" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "$env_file"
    set +a
  fi
}

request() {
  local method="$1"
  local url="$2"
  local body="$3"
  shift 3
  local -a headers=("$@")
  local response_file="$TMP_DIR/response-$RANDOM.json"
  local -a curl_args=(
    -sS
    --connect-timeout 3
    --max-time 15
    -o "$response_file"
    -w "%{http_code}"
    -X "$method"
    "$url"
  )

  if [[ -n "$body" ]]; then
    curl_args+=(-H "content-type: application/json" --data "$body")
  fi

  if [[ "${#headers[@]}" -gt 0 ]]; then
    curl_args+=("${headers[@]}")
  fi

  REQUEST_RESPONSE_FILE="$response_file"
  REQUEST_STATUS="$(curl "${curl_args[@]}")"
}

fail() {
  local message="$1"
  echo "FAIL: $message" >&2
  if [[ -n "$REQUEST_STATUS" ]]; then
    echo "HTTP status: $REQUEST_STATUS" >&2
  fi
  if [[ -n "$REQUEST_RESPONSE_FILE" && -f "$REQUEST_RESPONSE_FILE" ]]; then
    echo "Response body:" >&2
    cat "$REQUEST_RESPONSE_FILE" >&2
    echo >&2
  fi
  exit 1
}

expect_status() {
  local expected="$1"
  local label="$2"
  if [[ "$REQUEST_STATUS" != "$expected" ]]; then
    fail "$label returned HTTP $REQUEST_STATUS, expected $expected"
  fi
  echo "PASS: $label"
}

json_get() {
  local file="$1"
  local path="$2"
  python3 -c '
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as fh:
    data = json.load(fh)

value = data
for part in sys.argv[2].split("."):
    if not part:
        continue
    if isinstance(value, dict):
        value = value.get(part)
    else:
        value = None
        break

if value is None:
    print("")
elif isinstance(value, (dict, list)):
    print(json.dumps(value))
else:
    print(value)
' "$file" "$path"
}

json_array_length() {
  local file="$1"
  local path="$2"
  python3 -c '
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as fh:
    data = json.load(fh)

value = data
for part in sys.argv[2].split("."):
    if not part:
        continue
    if isinstance(value, dict):
        value = value.get(part)
    else:
        value = None
        break

if isinstance(value, list):
    print(len(value))
else:
    print(0)
' "$file" "$path"
}

check_health() {
  local label="$1"
  local url="$2"
  request GET "$url" ""
  expect_status "200" "$label health"
}

register_or_login_user() {
  local email="$1"
  local password="$2"
  local name="$3"
  local register_body=""
  local login_body=""
  local login_status=""

  register_body="$(printf '{"email":"%s","password":"%s","name":"%s"}' "$email" "$password" "$name")"
  request POST "$GATEWAY_URL/api/v1/auth/register" "$register_body"
  if [[ "$REQUEST_STATUS" == "201" ]]; then
    echo "PASS: auth register"
  elif [[ "$REQUEST_STATUS" == "409" ]]; then
    echo "PASS: auth register reused existing user"
  else
    fail "auth register failed"
  fi

  login_body="$(printf '{"email":"%s","password":"%s"}' "$email" "$password")"
  request POST "$GATEWAY_URL/api/v1/auth/login" "$login_body"
  login_status="$(json_get "$REQUEST_RESPONSE_FILE" "status")"
  if [[ "$REQUEST_STATUS" != "200" || "$login_status" != "authenticated" ]]; then
    return 1
  fi

  SMOKE_USER_EMAIL="$email"
  SMOKE_ACCESS_TOKEN="$(json_get "$REQUEST_RESPONSE_FILE" "access_token")"
  if [[ -z "$SMOKE_ACCESS_TOKEN" ]]; then
    fail "auth login did not return an access token"
  fi

  echo "PASS: auth login"
}

authenticate() {
  local primary_email="${OPENFOUNDRY_SMOKE_EMAIL:-smoke@openfoundry.local}"
  local password="${OPENFOUNDRY_SMOKE_PASSWORD:-openfoundry-smoke-password}"
  local name="${OPENFOUNDRY_SMOKE_NAME:-OpenFoundry Smoke}"
  local fallback_email=""

  if register_or_login_user "$primary_email" "$password" "$name"; then
    return
  fi

  fallback_email="smoke+$(date +%s)-$RANDOM@openfoundry.local"
  echo "INFO: primary smoke user could not log in cleanly, retrying with $fallback_email"
  if ! register_or_login_user "$fallback_email" "$password" "$name"; then
    fail "auth login failed for both primary and fallback smoke users"
  fi
}

main() {
  require_command curl
  require_command python3

  load_env

  GATEWAY_URL="${OPENFOUNDRY_GATEWAY_URL:-http://$(normalize_host "${GATEWAY_HOST:-127.0.0.1}"):${GATEWAY_PORT:-8080}}"
  AUTH_URL="${OPENFOUNDRY_AUTH_URL:-http://127.0.0.1:50051}"
  DATASET_URL="${OPENFOUNDRY_DATASET_URL:-http://127.0.0.1:50079}"
  ONTOLOGY_URL="${OPENFOUNDRY_ONTOLOGY_URL:-http://127.0.0.1:50057}"

  echo "Running OpenFoundry smoke checks..."
  echo "Gateway:  $GATEWAY_URL"
  echo "Auth:     $AUTH_URL"
  echo "Datasets: $DATASET_URL"
  echo "Ontology: $ONTOLOGY_URL"

  check_health "gateway" "$GATEWAY_URL/health"
  check_health "auth-service" "$AUTH_URL/health"
  check_health "dataset-service" "$DATASET_URL/health"
  check_health "ontology-service" "$ONTOLOGY_URL/health"

  authenticate

  request GET "$GATEWAY_URL/api/v1/users/me" "" -H "authorization: Bearer $SMOKE_ACCESS_TOKEN"
  expect_status "200" "gateway auth proxy"
  echo "PASS: authenticated user is $(json_get "$REQUEST_RESPONSE_FILE" "email")"

  request GET "$GATEWAY_URL/api/v1/datasets?page=1&per_page=5" "" -H "authorization: Bearer $SMOKE_ACCESS_TOKEN"
  expect_status "200" "gateway dataset proxy"
  echo "PASS: datasets listed (page items=$(json_array_length "$REQUEST_RESPONSE_FILE" "data"), total=$(json_get "$REQUEST_RESPONSE_FILE" "total"))"

  request GET "$GATEWAY_URL/api/v1/ontology/types?page=1&per_page=5" "" -H "authorization: Bearer $SMOKE_ACCESS_TOKEN"
  expect_status "200" "gateway ontology proxy"
  echo "PASS: ontology types listed (page items=$(json_array_length "$REQUEST_RESPONSE_FILE" "data"), total=$(json_get "$REQUEST_RESPONSE_FILE" "total"))"

  echo
  echo "Smoke checks completed successfully."
  echo "User: $SMOKE_USER_EMAIL"
}

main "$@"
