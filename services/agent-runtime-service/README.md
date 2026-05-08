# agent-runtime-service

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
