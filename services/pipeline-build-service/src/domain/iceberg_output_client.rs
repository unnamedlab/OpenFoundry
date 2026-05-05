//! HTTP-backed [`OutputTransactionClient`] for Foundry Iceberg outputs
//! (ADR-0041).
//!
//! Bridges the build executor's multi-output commit/abort callbacks to
//! `iceberg-catalog-service`'s spec endpoints. This is the productive
//! counterpart to the `MockOutputClient` used by the lifecycle tests.
//!
//! ## Routing
//!
//! Commit/abort calls are filtered by the dataset RID prefix:
//!
//! * `ri.foundry.main.iceberg-table.<id>` → routed to the catalog's
//!   `POST /iceberg/v1/transactions/commit` endpoint (P2 atomic
//!   multi-table commit, see ADR-0041 § Decision item 2).
//! * Anything else → no-op `Ok(())`. A future composing wrapper that
//!   knows about legacy non-Iceberg outputs delegates the no-op cases
//!   to its own backend; this client never claims responsibility for
//!   non-Iceberg dataset RIDs.
//!
//! ## Bootstrap
//!
//! The binary instantiates this client only when
//! `FOUNDRY_ICEBERG_CATALOG_URL` is set (see [`crate::main`]); when the
//! env var is absent, the executor still works with whatever legacy
//! client the rest of the wiring provides — but the catalog's
//! row-locked, all-or-nothing semantics aren't enforced. The
//! bootstrap path emits a `warn!` line in that case so operators see
//! the gap on startup.
//!
//! ## Why no abort
//!
//! The catalog's `/iceberg/v1/transactions/commit` is atomic server-side:
//! a failed commit rolls back the surrounding Postgres transaction
//! before returning, so there is nothing for the executor to undo on
//! the catalog side. `abort` therefore returns `Ok(())` immediately.

use async_trait::async_trait;
use serde_json::json;

use crate::domain::build_executor::{OutputClientError, OutputTransactionClient};

/// Iceberg dataset RIDs are minted by `iceberg-catalog-service` with
/// this prefix (see `services/iceberg-catalog-service/migrations/20260504000110_iceberg_init.sql`).
pub const ICEBERG_DATASET_RID_PREFIX: &str = "ri.foundry.main.iceberg-table.";

#[derive(Clone)]
pub struct IcebergOutputClient {
    base_url: String,
    bearer: Option<String>,
    http: reqwest::Client,
}

impl IcebergOutputClient {
    pub fn new(base_url: impl Into<String>, bearer: Option<String>, http: reqwest::Client) -> Self {
        Self {
            base_url: base_url.into().trim_end_matches('/').to_string(),
            bearer,
            http,
        }
    }

    fn commit_url(&self) -> String {
        format!("{}/iceberg/v1/transactions/commit", self.base_url)
    }

    fn handles(&self, dataset_rid: &str) -> bool {
        dataset_rid.starts_with(ICEBERG_DATASET_RID_PREFIX)
    }
}

#[async_trait]
impl OutputTransactionClient for IcebergOutputClient {
    async fn commit(
        &self,
        dataset_rid: &str,
        transaction_rid: &str,
    ) -> Result<(), OutputClientError> {
        if !self.handles(dataset_rid) {
            return Ok(());
        }
        let body = json!({
            "table-changes": [],
            "build_rid": transaction_rid,
        });
        let mut req = self.http.post(self.commit_url()).json(&body);
        if let Some(token) = &self.bearer {
            req = req.bearer_auth(token);
        }
        let response = req.send().await.map_err(|err| {
            OutputClientError(format!(
                "iceberg commit {dataset_rid}/{transaction_rid}: {err}"
            ))
        })?;
        let status = response.status();
        if !status.is_success() {
            let body = response.text().await.unwrap_or_default();
            return Err(OutputClientError(format!(
                "iceberg commit {dataset_rid}/{transaction_rid} returned {status}: {body}"
            )));
        }
        Ok(())
    }

    async fn abort(
        &self,
        _dataset_rid: &str,
        _transaction_rid: &str,
    ) -> Result<(), OutputClientError> {
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn commit_skips_non_iceberg_dataset_rids() {
        let client =
            IcebergOutputClient::new("http://unreachable.invalid", None, reqwest::Client::new());
        client
            .commit(
                "ri.foundry.main.dataset.legacy",
                "ri.foundry.main.transaction.x",
            )
            .await
            .expect("non-iceberg commit must be a noop");
    }

    #[tokio::test]
    async fn abort_is_always_a_noop() {
        let client =
            IcebergOutputClient::new("http://unreachable.invalid", None, reqwest::Client::new());
        client
            .abort(
                "ri.foundry.main.iceberg-table.00000000-0000-0000-0000-000000000001",
                "ri.foundry.main.transaction.x",
            )
            .await
            .expect("abort must always succeed");
    }
}
