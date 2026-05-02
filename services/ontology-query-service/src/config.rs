//! Environment-driven configuration for `ontology-query-service`.

use std::time::Duration;

use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,

    /// Cassandra contact points (`host:port,host:port,…`). Empty
    /// ⇒ in-memory `ObjectStore` (smoke tests / local dev). Production
    /// MUST set this.
    #[serde(default)]
    pub cassandra_contact_points: String,
    #[serde(default = "default_local_dc")]
    pub cassandra_local_dc: String,

    /// NATS URL for the invalidation subscriber. Empty ⇒ subscriber
    /// is not started (cache will only invalidate on local writes,
    /// which the read service never issues — i.e. all reads will be
    /// served from cache until TTL expiry).
    #[serde(default)]
    pub nats_url: String,

    /// Cache capacity (entries). 0 ⇒ default 100 000 (plan §S1.5.a).
    #[serde(default)]
    pub cache_capacity: u64,

    /// Cache TTL (seconds). 0 ⇒ default 30 s (plan §S1.5.a).
    #[serde(default)]
    pub cache_ttl_seconds: u64,
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }

    pub fn cache_ttl(&self) -> Duration {
        if self.cache_ttl_seconds == 0 {
            crate::cache::DEFAULT_TTL
        } else {
            Duration::from_secs(self.cache_ttl_seconds)
        }
    }

    pub fn cache_capacity_or_default(&self) -> u64 {
        if self.cache_capacity == 0 {
            crate::cache::DEFAULT_CAPACITY
        } else {
            self.cache_capacity
        }
    }
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50104
}

fn default_local_dc() -> String {
    "dc1".to_string()
}
