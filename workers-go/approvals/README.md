# `approvals` worker

Task queue: `openfoundry.approvals`. Hosts the
`ApprovalRequestWorkflow` (S2.5). State lives in Temporal —
durable, signalable, queryable. The legacy `approvals.*` tables in
Postgres are dropped by S2.5.d once the cutover is complete.

## Configuration

| env var | default | purpose |
|---|---|---|
| `TEMPORAL_ADDRESS` | `127.0.0.1:7233` | Frontend gRPC; preferred Helm value |
| `TEMPORAL_HOST_PORT` | `127.0.0.1:7233` | Frontend gRPC fallback |
| `TEMPORAL_NAMESPACE` | `default` | Namespace |
| `TEMPORAL_TASK_QUEUE` | `openfoundry.approvals` | Task queue polled by this worker |
| `OF_AUDIT_COMPLIANCE_URL` | `http://audit-compliance-service:50115` | Base URL used by `EmitAuditEvent` for `POST /api/v1/audit/events` |
| `OF_AUDIT_BEARER_TOKEN` | _(empty)_ | Optional service bearer token forwarded to audit-compliance-service |

`OF_AUDIT_URL`, `AUDIT_COMPLIANCE_SERVICE_URL`,
`AUDIT_SERVICE_URL`, and the older `OF_AUDIT_GRPC_ADDR` are accepted
as rollout fallbacks while manifests converge on the HTTP env name.
