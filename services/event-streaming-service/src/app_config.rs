//! Runtime configuration for the routing facade.
//!
//! Values are loaded from the process environment using the `EVENT_ROUTER__*`
//! prefix (the double underscore acts as a nesting separator, which keeps the
//! convention consistent with the rest of OpenFoundry's services).

use serde::Deserialize;

/// Top-level service configuration.
#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    /// Bind address for both the gRPC server and the side router.
    #[serde(default = "default_host")]
    pub host: String,
    /// gRPC port (`Publish` / `Subscribe`).
    #[serde(default = "default_grpc_port")]
    pub grpc_port: u16,
    /// HTTP side router port for `/healthz` and `/metrics`.
    #[serde(default = "default_admin_port")]
    pub admin_port: u16,
    /// Path to the declarative routing table (`topic-routes.yaml`).
    #[serde(default = "default_routes_file")]
    pub routes_file: String,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_grpc_port() -> u16 {
    50121
}

fn default_admin_port() -> u16 {
    50122
}

fn default_routes_file() -> String {
    "config/topic-routes.yaml".to_string()
}

impl AppConfig {
    /// Load configuration from the process environment.
    ///
    /// All keys are case-insensitive and use `__` as the nesting separator,
    /// e.g. `EVENT_ROUTER__GRPC_PORT=50121`.
    pub fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(
                config::Environment::with_prefix("EVENT_ROUTER")
                    .separator("__")
                    .try_parsing(true),
            )
            .build()?
            .try_deserialize()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn defaults_are_sensible() {
        // We can't easily roundtrip through Environment without polluting the
        // process env, so just exercise the Default-style builder via serde.
        let cfg: AppConfig = serde_json::from_value(serde_json::json!({})).unwrap();
        assert_eq!(cfg.host, "0.0.0.0");
        assert_eq!(cfg.grpc_port, 50121);
        assert_eq!(cfg.admin_port, 50122);
        assert_eq!(cfg.routes_file, "config/topic-routes.yaml");
    }
}
