# `approvals` worker

Task queue: `openfoundry.approvals`. Hosts the
`ApprovalRequestWorkflow` (S2.5). State lives in Temporal —
durable, signalable, queryable. The legacy `approvals.*` tables in
Postgres are dropped by S2.5.d once the cutover is complete.
