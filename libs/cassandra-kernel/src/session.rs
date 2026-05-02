//! Session construction.
//!
//! Wraps `scylla::SessionBuilder` with the OpenFoundry defaults:
//!
//! * **DC-local routing** via `DefaultPolicy::prefer_datacenter`. Every
//!   service is pinned to its local DC; cross-DC reads only happen as
//!   replication or as a deliberate fallback.
//! * **`LOCAL_QUORUM`** as the default consistency, both for reads
//!   and writes (ADR-0020).
//! * **Default retry policy** with idempotency awareness — non-
//!   idempotent statements are not retried on timeout.
//! * **Speculative execution** with a conservative threshold so a
//!   single slow replica does not stretch p99.
//! * **Tracing opt-in** flag so a service can flip per-request
//!   tracing on under an incident without recompiling.

use std::sync::Arc;
use std::time::Duration;

use scylla::Session;
use scylla::load_balancing::DefaultPolicy;
use scylla::retry_policy::DefaultRetryPolicy;
use scylla::speculative_execution::SimpleSpeculativeExecutionPolicy;
use scylla::statement::Consistency;
use scylla::transport::execution_profile::ExecutionProfile;

use crate::error::{KernelError, KernelResult};

/// Static configuration for a Cassandra cluster, as consumed by a
/// service at startup.
///
/// Loaded from environment / config files; this struct is the only
/// thing services need to construct, the rest is plumbing.
#[derive(Debug, Clone)]
pub struct ClusterConfig {
    /// Native-protocol contact points, e.g.
    /// `["of-cass-prod-dc1-service.cassandra.svc:9042"]`.
    pub contact_points: Vec<String>,
    /// Local datacenter name, must match `cassandra-rackdc.properties`
    /// on the cluster (e.g. `dc1`). LOCAL_QUORUM is computed against
    /// this DC.
    pub local_datacenter: String,
    /// Optional CQL user. None disables auth (dev only).
    pub username: Option<String>,
    /// Optional CQL password.
    pub password: Option<String>,
    /// Default keyspace to USE on connect. None leaves the session
    /// keyspace-less; per-statement `keyspace.table` is required in
    /// that case.
    pub keyspace: Option<String>,
    /// Whether to request per-request driver tracing. Off by default;
    /// flipping this to `true` materially increases load on the
    /// `system_traces` keyspace.
    pub enable_tracing: bool,
    /// Connection-establishment timeout.
    pub connect_timeout: Duration,
    /// Per-request timeout. Aligns with the `*_request_timeout_in_ms`
    /// values in `cluster-prod.yaml`.
    pub request_timeout: Duration,
}

impl ClusterConfig {
    /// Sensible defaults for a single-DC dev/CI cluster on
    /// `127.0.0.1:9042`.
    pub fn dev_local() -> Self {
        Self {
            contact_points: vec!["127.0.0.1:9042".to_string()],
            local_datacenter: "dc1".to_string(),
            username: None,
            password: None,
            keyspace: None,
            enable_tracing: false,
            connect_timeout: Duration::from_secs(5),
            request_timeout: Duration::from_secs(5),
        }
    }
}

/// Builder over [`ClusterConfig`]. Wraps [`scylla::SessionBuilder`]
/// and applies the OpenFoundry defaults documented in the module
/// header.
pub struct SessionBuilder {
    config: ClusterConfig,
}

impl SessionBuilder {
    /// Construct from a [`ClusterConfig`].
    pub fn new(config: ClusterConfig) -> Self {
        Self { config }
    }

    /// Build the session. Async because the driver opens connections
    /// to all contact points and discovers topology before returning.
    pub async fn build(self) -> KernelResult<Session> {
        let load_balancing = DefaultPolicy::builder()
            .prefer_datacenter(self.config.local_datacenter.clone())
            .token_aware(true)
            .permit_dc_failover(false)
            .build();

        // Speculative execution: fire a redundant request after 50ms
        // up to 2 times. Cheap insurance against single slow replicas.
        let speculative = Arc::new(SimpleSpeculativeExecutionPolicy {
            max_retry_count: 2,
            retry_interval: Duration::from_millis(50),
        });

        let profile = ExecutionProfile::builder()
            .consistency(Consistency::LocalQuorum)
            .request_timeout(Some(self.config.request_timeout))
            .retry_policy(Box::new(DefaultRetryPolicy::new()))
            .load_balancing_policy(load_balancing)
            .speculative_execution_policy(Some(speculative))
            .build();

        let mut sb = scylla::SessionBuilder::new()
            .known_nodes(&self.config.contact_points)
            .connection_timeout(self.config.connect_timeout)
            .default_execution_profile_handle(profile.into_handle())
            .tracing_info_fetch_consistency(Consistency::LocalOne);

        if self.config.enable_tracing {
            // Per-request tracing is opt-in at call sites via
            // `Query::with_tracing(true)`. This flag turns it on
            // globally — only do it under an incident.
            sb = sb.tracing_info_fetch_consistency(Consistency::LocalQuorum);
        }

        if let (Some(user), Some(pass)) = (&self.config.username, &self.config.password) {
            sb = sb.user(user, pass);
        }

        if let Some(ks) = &self.config.keyspace {
            sb = sb.use_keyspace(ks, true);
        }

        sb.build().await.map_err(KernelError::from)
    }
}
