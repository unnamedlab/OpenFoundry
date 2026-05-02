//! `approvals-service` — Temporal-backed approval queue.
//!
//! ## Status: post-S2.5 substrate
//!
//! Per Stream **S2.5** of
//! `docs/architecture/migration-plan-cassandra-foundry-parity.md`,
//! the durable state of an approval (pending → approved/rejected/expired)
//! lives in **Temporal** as an [`ApprovalRequestWorkflow`] execution
//! (signal `decide`, 24h selector timeout). The Postgres table
//! `workflow_approvals` is **deprecated** and its CRUD path is being
//! retired PR-by-PR; new code paths must go through
//! [`domain::temporal_adapter::ApprovalsAdapter`] instead.
//!
//! [`ApprovalRequestWorkflow`]: https://github.com/openfoundry/openfoundry/tree/main/workers-go/approvals/workflows/approval_request.go

pub mod domain {
    pub mod temporal_adapter;
}
