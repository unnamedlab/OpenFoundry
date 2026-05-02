//! Process-wide singleton holder for `Arc<Session>`.
//!
//! Cassandra connections are expensive to set up (the driver opens
//! one TCP connection per node and pools shards). Services should
//! create exactly one `Session` per process and clone the `Arc`
//! everywhere else.
//!
//! Use [`SharedSession`] as a `static` in your service crate:
//!
//! ```ignore
//! use cassandra_kernel::{ClusterConfig, SharedSession};
//!
//! static SESSION: SharedSession = SharedSession::new();
//!
//! pub async fn init(cfg: ClusterConfig) -> anyhow::Result<()> {
//!     SESSION.init(cfg).await?;
//!     Ok(())
//! }
//!
//! pub fn session() -> std::sync::Arc<scylla::Session> {
//!     SESSION.get().expect("session not initialised")
//! }
//! ```

use std::sync::Arc;

use scylla::Session;
use tokio::sync::OnceCell;

use crate::error::KernelResult;
use crate::session::{ClusterConfig, SessionBuilder};

/// Holder for the per-process `Arc<Session>`. Safe to declare as a
/// `static` thanks to [`OnceCell`].
pub struct SharedSession {
    cell: OnceCell<Arc<Session>>,
}

impl SharedSession {
    /// Construct an empty holder. Intended for `static` initialisers.
    pub const fn new() -> Self {
        Self {
            cell: OnceCell::const_new(),
        }
    }

    /// Initialise the session if it has not been initialised yet.
    /// Returns the `Arc<Session>` either way. Safe to call multiple
    /// times concurrently — only the first call performs the actual
    /// connect.
    pub async fn init(&self, config: ClusterConfig) -> KernelResult<Arc<Session>> {
        let session = self
            .cell
            .get_or_try_init(|| async move {
                let s = SessionBuilder::new(config).build().await?;
                Ok::<_, crate::error::KernelError>(Arc::new(s))
            })
            .await?;
        Ok(session.clone())
    }

    /// Get the initialised session, if any. Returns `None` before the
    /// first successful [`Self::init`].
    pub fn get(&self) -> Option<Arc<Session>> {
        self.cell.get().cloned()
    }
}

impl Default for SharedSession {
    fn default() -> Self {
        Self::new()
    }
}
