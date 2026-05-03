//! Loader and validator for `topic-routes.yaml`.
//!
//! The YAML schema is intentionally small:
//!
//! ```yaml
//! default_backend: nats        # optional
//! backends:
//!   nats:
//!     url: nats://localhost:4222
//!     stream: events           # optional, used when JetStream publishing
//!   kafka:
//!     brokers: ["localhost:9092"]
//!     client_id: event-router  # optional
//! routes:
//!   - pattern: "ctrl.*"
//!     backend: nats
//!   - pattern: "data.>"
//!     backend: kafka
//!     schema_id: "data.orders.v1"   # optional, reserved for PR 3
//!     dlq: "dlq.data"               # optional, reserved for future use
//! ```
//!
//! Pattern syntax matches NATS subject wildcards:
//!
//! * `*` matches exactly one token (between dots).
//! * `>` matches one or more trailing tokens. Must appear last.
//! * Otherwise the pattern is matched literally.
//!
//! Validation performed on load:
//!
//! * Every `backend` reference must exist in the `backends` map.
//! * `default_backend`, when set, must exist in the `backends` map.
//! * No two routes may declare the same `pattern`.
//! * Patterns must compile to a valid regex.

use std::collections::BTreeSet;
use std::path::{Path, PathBuf};

use serde::Deserialize;
use thiserror::Error;

use super::table::{BackendId, CompiledRoute, RouteTable};

/// Errors raised while loading or validating the routing table.
#[derive(Debug, Error)]
pub enum ConfigError {
    #[error("failed to read routing table from {path}: {source}")]
    Io {
        path: PathBuf,
        #[source]
        source: std::io::Error,
    },
    #[error("failed to parse routing table: {0}")]
    Parse(#[from] serde_yaml::Error),
    #[error("route #{index} references backend `{backend}` which is not declared in `backends`")]
    UnknownBackend { index: usize, backend: String },
    #[error("`default_backend` references backend `{0}` which is not declared in `backends`")]
    UnknownDefaultBackend(String),
    #[error("duplicate route pattern `{pattern}`")]
    DuplicatePattern { pattern: String },
    #[error("invalid pattern `{pattern}`: {source}")]
    InvalidPattern {
        pattern: String,
        #[source]
        source: regex::Error,
    },
    #[error("at least one route or a `default_backend` must be configured")]
    Empty,
    #[error("`backends.{backend}` is missing required field `{field}`")]
    MissingBackendField {
        backend: String,
        field: &'static str,
    },
}

/// Top-level shape of `topic-routes.yaml`.
#[derive(Debug, Clone, Deserialize)]
pub struct RouterConfig {
    #[serde(default)]
    pub default_backend: Option<String>,
    pub backends: BackendsConfig,
    #[serde(default)]
    pub routes: Vec<RouteEntry>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct BackendsConfig {
    #[serde(default)]
    pub nats: Option<NatsBackendConfig>,
    #[serde(default)]
    pub kafka: Option<KafkaBackendConfig>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct NatsBackendConfig {
    pub url: String,
    #[serde(default)]
    pub stream: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct KafkaBackendConfig {
    #[serde(default)]
    pub brokers: Vec<String>,
    #[serde(default)]
    pub client_id: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RouteEntry {
    pub pattern: String,
    pub backend: String,
    #[serde(default)]
    pub schema_id: Option<String>,
    #[serde(default)]
    pub dlq: Option<String>,
}

impl RouterConfig {
    /// Load and validate the routing table from disk.
    pub fn load(path: impl AsRef<Path>) -> Result<Self, ConfigError> {
        let path = path.as_ref();
        let raw = std::fs::read_to_string(path).map_err(|source| ConfigError::Io {
            path: path.to_path_buf(),
            source,
        })?;
        Self::from_yaml(&raw)
    }

    /// Parse + validate a YAML payload.
    pub fn from_yaml(raw: &str) -> Result<Self, ConfigError> {
        let cfg: RouterConfig = serde_yaml::from_str(raw)?;
        cfg.validate()?;
        Ok(cfg)
    }

    fn validate(&self) -> Result<(), ConfigError> {
        if self.routes.is_empty() && self.default_backend.is_none() {
            return Err(ConfigError::Empty);
        }

        // Validate `default_backend` reference.
        if let Some(name) = &self.default_backend {
            self.resolve_backend(name)
                .ok_or_else(|| ConfigError::UnknownDefaultBackend(name.clone()))?;
        }

        // Validate route references and detect duplicates.
        let mut seen = BTreeSet::new();
        for (index, route) in self.routes.iter().enumerate() {
            if !seen.insert(route.pattern.clone()) {
                return Err(ConfigError::DuplicatePattern {
                    pattern: route.pattern.clone(),
                });
            }
            self.resolve_backend(&route.backend)
                .ok_or_else(|| ConfigError::UnknownBackend {
                    index,
                    backend: route.backend.clone(),
                })?;
        }

        // Validate concrete per-backend fields when the backend is referenced.
        if self.is_backend_referenced("nats") {
            let nats = self
                .backends
                .nats
                .as_ref()
                .ok_or(ConfigError::MissingBackendField {
                    backend: "nats".into(),
                    field: "url",
                })?;
            if nats.url.is_empty() {
                return Err(ConfigError::MissingBackendField {
                    backend: "nats".into(),
                    field: "url",
                });
            }
        }
        if self.is_backend_referenced("kafka") {
            let kafka = self
                .backends
                .kafka
                .as_ref()
                .ok_or(ConfigError::MissingBackendField {
                    backend: "kafka".into(),
                    field: "brokers",
                })?;
            if kafka.brokers.is_empty() {
                return Err(ConfigError::MissingBackendField {
                    backend: "kafka".into(),
                    field: "brokers",
                });
            }
        }

        Ok(())
    }

    fn is_backend_referenced(&self, name: &str) -> bool {
        self.default_backend.as_deref() == Some(name)
            || self.routes.iter().any(|r| r.backend == name)
    }

    fn resolve_backend(&self, name: &str) -> Option<BackendId> {
        match name {
            "nats" => self.backends.nats.as_ref().map(|_| BackendId::Nats),
            "kafka" => self.backends.kafka.as_ref().map(|_| BackendId::Kafka),
            _ => None,
        }
    }

    /// Compile this declarative config into an executable [`RouteTable`].
    pub fn compile(&self) -> Result<RouteTable, ConfigError> {
        let mut entries = Vec::with_capacity(self.routes.len());
        for route in &self.routes {
            // Backend was already validated above, but resolve again for safety.
            let backend = self
                .resolve_backend(&route.backend)
                .expect("validate() enforces resolvable backend");
            entries.push(
                CompiledRoute::compile(
                    route.pattern.clone(),
                    backend,
                    route.schema_id.clone(),
                    route.dlq.clone(),
                )
                .map_err(|source| ConfigError::InvalidPattern {
                    pattern: route.pattern.clone(),
                    source,
                })?,
            );
        }
        let default = self
            .default_backend
            .as_deref()
            .and_then(|name| self.resolve_backend(name));
        Ok(RouteTable::new(entries, default))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn yaml_with(routes: &str) -> String {
        format!(
            "backends:\n  nats:\n    url: nats://localhost:4222\n  kafka:\n    brokers: [\"localhost:9092\"]\nroutes:\n{routes}"
        )
    }

    #[test]
    fn parses_valid_config() {
        let yaml = yaml_with(
            "  - pattern: \"ctrl.*\"\n    backend: nats\n  - pattern: \"data.>\"\n    backend: kafka\n",
        );
        let cfg = RouterConfig::from_yaml(&yaml).expect("valid config");
        assert_eq!(cfg.routes.len(), 2);
        assert_eq!(cfg.routes[0].backend, "nats");
        assert!(cfg.default_backend.is_none());
        assert!(cfg.compile().is_ok());
    }

    #[test]
    fn rejects_unknown_backend_in_route() {
        let yaml = "backends:\n  nats:\n    url: nats://x:4222\nroutes:\n  - pattern: \"a.*\"\n    backend: kafka\n";
        let err = RouterConfig::from_yaml(yaml).unwrap_err();
        assert!(
            matches!(err, ConfigError::UnknownBackend { index: 0, .. }),
            "{err:?}"
        );
    }

    #[test]
    fn rejects_unknown_default_backend() {
        let yaml = "default_backend: kafka\nbackends:\n  nats:\n    url: nats://x:4222\nroutes:\n  - pattern: \"a.*\"\n    backend: nats\n";
        let err = RouterConfig::from_yaml(yaml).unwrap_err();
        assert!(
            matches!(err, ConfigError::UnknownDefaultBackend(_)),
            "{err:?}"
        );
    }

    #[test]
    fn rejects_duplicate_pattern() {
        let yaml = yaml_with(
            "  - pattern: \"ctrl.*\"\n    backend: nats\n  - pattern: \"ctrl.*\"\n    backend: kafka\n",
        );
        let err = RouterConfig::from_yaml(&yaml).unwrap_err();
        assert!(
            matches!(err, ConfigError::DuplicatePattern { .. }),
            "{err:?}"
        );
    }

    #[test]
    fn rejects_empty_when_no_default() {
        let yaml = "backends:\n  nats:\n    url: nats://x:4222\nroutes: []\n";
        let err = RouterConfig::from_yaml(yaml).unwrap_err();
        assert!(matches!(err, ConfigError::Empty), "{err:?}");
    }

    #[test]
    fn accepts_default_only() {
        let yaml =
            "default_backend: nats\nbackends:\n  nats:\n    url: nats://x:4222\nroutes: []\n";
        let cfg = RouterConfig::from_yaml(yaml).expect("valid");
        let table = cfg.compile().unwrap();
        assert!(table.is_empty());
        assert_eq!(table.default_backend(), Some(BackendId::Nats));
    }

    #[test]
    fn rejects_missing_kafka_brokers() {
        let yaml = "backends:\n  kafka:\n    brokers: []\nroutes:\n  - pattern: \"a.*\"\n    backend: kafka\n";
        let err = RouterConfig::from_yaml(yaml).unwrap_err();
        assert!(
            matches!(err, ConfigError::MissingBackendField { .. }),
            "{err:?}"
        );
    }
}
