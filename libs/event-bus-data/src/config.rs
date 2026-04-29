//! Kafka client configuration for the data plane.
//!
//! All client builders exposed here disable topic auto-creation and require an
//! explicit per-service SASL principal so brokers can enforce ACLs at the
//! authorization layer.

use rdkafka::ClientConfig;

/// SASL principal a service uses to authenticate against the broker.
///
/// The platform provisions one principal per service so Kafka ACLs (e.g.
/// `Allow Read`/`Allow Write` on a topic prefix) can be granted by service
/// identity rather than by IP or shared credentials.
#[derive(Debug, Clone)]
pub struct ServicePrincipal {
    /// Service name, used both as the SASL username and as the
    /// `client.id`/`group.id` prefix for observability and ACLs.
    pub service: String,
    /// SASL password (typically a short-lived secret rotated by the
    /// platform's secret broker).
    pub password: String,
    /// SASL mechanism. Defaults to `SCRAM-SHA-512`.
    pub mechanism: String,
    /// Security protocol. Defaults to `SASL_SSL`. Set to `PLAINTEXT` for
    /// development brokers without TLS (e.g. testcontainers).
    pub security_protocol: String,
}

impl ServicePrincipal {
    /// Build a production-style principal using SCRAM-SHA-512 over SASL_SSL.
    pub fn scram_sha_512(service: impl Into<String>, password: impl Into<String>) -> Self {
        Self {
            service: service.into(),
            password: password.into(),
            mechanism: "SCRAM-SHA-512".to_string(),
            security_protocol: "SASL_SSL".to_string(),
        }
    }

    /// Build a development principal that talks to an unauthenticated broker.
    /// Intended for `testcontainers`-style integration tests only.
    pub fn insecure_dev(service: impl Into<String>) -> Self {
        Self {
            service: service.into(),
            password: String::new(),
            mechanism: String::new(),
            security_protocol: "PLAINTEXT".to_string(),
        }
    }

    fn apply(&self, cfg: &mut ClientConfig) {
        cfg.set("client.id", &self.service);
        cfg.set("security.protocol", &self.security_protocol);
        if !self.mechanism.is_empty() {
            cfg.set("sasl.mechanism", &self.mechanism);
            cfg.set("sasl.username", &self.service);
            cfg.set("sasl.password", &self.password);
        }
    }
}

/// Top-level configuration for the data plane bus.
#[derive(Debug, Clone)]
pub struct DataBusConfig {
    /// Comma-separated `host:port` list of bootstrap brokers.
    pub bootstrap_servers: String,
    /// Service identity used for SASL/ACL.
    pub principal: ServicePrincipal,
    /// Producer/consumer request timeout in milliseconds.
    pub request_timeout_ms: u32,
}

impl DataBusConfig {
    pub fn new(bootstrap_servers: impl Into<String>, principal: ServicePrincipal) -> Self {
        Self {
            bootstrap_servers: bootstrap_servers.into(),
            principal,
            request_timeout_ms: 30_000,
        }
    }

    /// Build a producer-side `ClientConfig` with auto-create disabled and
    /// at-least-once-friendly defaults (`acks=all`, idempotence enabled).
    pub fn producer_config(&self) -> ClientConfig {
        let mut cfg = ClientConfig::new();
        cfg.set("bootstrap.servers", &self.bootstrap_servers);
        cfg.set("allow.auto.create.topics", "false");
        cfg.set("enable.idempotence", "true");
        cfg.set("acks", "all");
        cfg.set("compression.type", "zstd");
        cfg.set("request.timeout.ms", self.request_timeout_ms.to_string());
        // librdkafka's idempotent producer requires bounded in-flight requests.
        cfg.set("max.in.flight.requests.per.connection", "5");
        self.principal.apply(&mut cfg);
        cfg
    }

    /// Build a consumer-side `ClientConfig` with auto-create disabled and
    /// auto-commit disabled (we expose explicit commits at the trait level).
    pub fn consumer_config(&self, group_id: &str) -> ClientConfig {
        let mut cfg = ClientConfig::new();
        cfg.set("bootstrap.servers", &self.bootstrap_servers);
        cfg.set("group.id", group_id);
        cfg.set("allow.auto.create.topics", "false");
        cfg.set("enable.auto.commit", "false");
        cfg.set("enable.partition.eof", "false");
        cfg.set("auto.offset.reset", "earliest");
        cfg.set("session.timeout.ms", "10000");
        cfg.set("isolation.level", "read_committed");
        self.principal.apply(&mut cfg);
        cfg
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn producer_config_disables_auto_create_and_enables_idempotence() {
        let cfg = DataBusConfig::new(
            "broker:9092",
            ServicePrincipal::scram_sha_512("orders-svc", "secret"),
        )
        .producer_config();
        assert_eq!(cfg.get("allow.auto.create.topics"), Some("false"));
        assert_eq!(cfg.get("enable.idempotence"), Some("true"));
        assert_eq!(cfg.get("acks"), Some("all"));
        assert_eq!(cfg.get("client.id"), Some("orders-svc"));
        assert_eq!(cfg.get("sasl.mechanism"), Some("SCRAM-SHA-512"));
    }

    #[test]
    fn consumer_config_disables_auto_commit_and_uses_per_service_principal() {
        let cfg = DataBusConfig::new(
            "broker:9092",
            ServicePrincipal::scram_sha_512("orders-svc", "secret"),
        )
        .consumer_config("orders-cg");
        assert_eq!(cfg.get("enable.auto.commit"), Some("false"));
        assert_eq!(cfg.get("allow.auto.create.topics"), Some("false"));
        assert_eq!(cfg.get("group.id"), Some("orders-cg"));
        assert_eq!(cfg.get("isolation.level"), Some("read_committed"));
    }

    #[test]
    fn dev_principal_uses_plaintext_and_omits_sasl() {
        let cfg = DataBusConfig::new("localhost:9092", ServicePrincipal::insecure_dev("svc"))
            .producer_config();
        assert_eq!(cfg.get("security.protocol"), Some("PLAINTEXT"));
        assert_eq!(cfg.get("sasl.mechanism"), None);
    }
}
