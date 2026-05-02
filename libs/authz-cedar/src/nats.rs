//! NATS subscriber that triggers `PolicyStore` hot-reload.
//!
//! Subscribes to the `authz.policy.changed` subject (configurable)
//! and, on every message, invokes the supplied `reload` closure.
//! Messages are intentionally treated as **signals only** — the
//! payload is ignored — so policy authors can publish anything
//! (typically an empty body or a `{"version": …}` blob) and still
//! get the engine to re-pull from `pg-policy.cedar_policies`.
//!
//! The subscriber is feature-gated behind `nats` so the base crate
//! stays sqlx/Postgres-only.

use std::{future::Future, pin::Pin, sync::Arc};

use async_nats::Client;
use futures::StreamExt;

/// Default subject the subscriber listens on.
pub const DEFAULT_SUBJECT: &str = "authz.policy.changed";

/// Async closure type returned by the user's reload callback.
type ReloadFut = Pin<Box<dyn Future<Output = anyhow::Result<usize>> + Send>>;

/// Listens on `authz.policy.changed` and re-pulls the policy bundle
/// every time a message arrives.
///
/// Drop the returned [`PolicyReloadHandle`] to stop the subscriber.
pub struct PolicyReloadSubscriber {
    client: Client,
    subject: String,
}

impl PolicyReloadSubscriber {
    pub fn new(client: Client) -> Self {
        Self {
            client,
            subject: DEFAULT_SUBJECT.to_string(),
        }
    }

    pub fn with_subject(mut self, subject: impl Into<String>) -> Self {
        self.subject = subject.into();
        self
    }

    /// Start the subscriber on a background task. The closure is
    /// called once per inbound message.
    pub async fn run<F>(self, reload: F) -> anyhow::Result<PolicyReloadHandle>
    where
        F: Fn() -> ReloadFut + Send + Sync + 'static,
    {
        let mut sub = self.client.subscribe(self.subject.clone()).await?;
        let subject = self.subject;
        let reload = Arc::new(reload);
        let task = tokio::spawn(async move {
            tracing::info!(%subject, "cedar policy reload subscriber started");
            while let Some(msg) = sub.next().await {
                tracing::debug!(
                    subject = %msg.subject,
                    bytes = msg.payload.len(),
                    "cedar policy reload signal received"
                );
                match (reload)().await {
                    Ok(count) => {
                        tracing::info!(policies = count, "cedar policies reloaded");
                    }
                    Err(error) => {
                        tracing::error!(
                            error = %error,
                            "cedar policy reload failed; keeping previous bundle"
                        );
                    }
                }
            }
            tracing::info!(%subject, "cedar policy reload subscriber stopped");
        });
        Ok(PolicyReloadHandle { task })
    }
}

/// Drop guard for the spawned subscriber task.
pub struct PolicyReloadHandle {
    task: tokio::task::JoinHandle<()>,
}

impl PolicyReloadHandle {
    /// Abort the background task. Safe to call multiple times.
    pub fn shutdown(self) {
        self.task.abort();
    }
}

/// Helper to wrap a `PgPolicyStore::reload`-style call into the
/// boxed-future signature expected by [`PolicyReloadSubscriber::run`].
#[macro_export]
macro_rules! reload_closure {
    ($store:expr) => {{
        let store = $store.clone();
        move || {
            let store = store.clone();
            Box::pin(async move { store.reload().await.map_err(anyhow::Error::from) })
                as ::std::pin::Pin<
                    Box<dyn ::std::future::Future<Output = anyhow::Result<usize>> + Send>,
                >
        }
    }};
}
