# agent-runtime-service

## LLM quick context (current code)

Hosts OpenAI-compatible agent/chat runtime endpoints and agent runtime state backed by libs/ai-kernel-go.

Agent note: use this for live agent sessions, prompts placeholder routes, and purpose-checkpoint checks.

Current surface:
- `/api/v1/agent-runtime/*`
- `/api/v1/agent-runtime/logic/functions/*`
- `/api/v1/ai/prompts* (placeholder)`
- `GET /healthz`
- `GET /metrics`

State/dependency hints:
- Contains `13` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `aievents`, `config`, `handlers`, `models`, `repo`, `server`.
- Local service files present: `Dockerfile`.

Configuration signals:
Environment variables referenced by the code:
- `ALLOW_FAKE_LLM_PROVIDER`, `AUTHORIZATION_POLICY_SERVICE_URL`, `DATABASE_URL`, `HOST`, `JWT_SECRET`, `KAFKA_BOOTSTRAP_SERVERS`, `PORT`, `PURPOSE_CHECKPOINT_URL`
- `SERVICE_VERSION`

Keep this section in sync when changing routes, config, or persistence behavior.

Hosts the agent runtime API and OpenAI-compatible chat/copilot endpoints backed by `libs/ai-kernel-go` domain runtimes.

## Environment

| Variable | Required | Description |
| --- | --- | --- |
| `DATABASE_URL` | yes | PostgreSQL connection used for agent runtime state and migrations. |
| `JWT_SECRET` | yes | Shared JWT signing secret for authenticated `/api/v1/agent-runtime/*` routes. |
| `KAFKA_BOOTSTRAP_SERVERS` | no | Kafka bootstrap servers for the future `ai.events.v1` producer wiring. |
| `AUTHORIZATION_POLICY_SERVICE_URL` | recommended | Base URL for authorization-policy-service purpose-checkpoint enforcement; used before live sensitive AI chat requests that set `require_private_network=true`. |
| `PURPOSE_CHECKPOINT_URL` | fallback | Alternate base URL for the same purpose-checkpoint service when `AUTHORIZATION_POLICY_SERVICE_URL` is not set. |

When neither `AUTHORIZATION_POLICY_SERVICE_URL` nor `PURPOSE_CHECKPOINT_URL` is configured, the service starts with purpose-checkpoint enforcement disabled and logs a warning.
