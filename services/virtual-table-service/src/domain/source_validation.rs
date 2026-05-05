//! Foundry-worker / egress-policy enforcement for virtual tables.
//!
//! Source of truth: Foundry doc § "Limitations of using virtual tables"
//! inside `Data connectivity & integration/Core concepts/Virtual tables.md`.
//!
//! The doc lists five hard "not supported" rules that must be enforced
//! at registration time:
//!
//!   1. Connections using an **agent worker** are not supported
//!      (only `FOUNDRY_WORKER` sources can back virtual tables).
//!   2. Connections using **agent proxy egress policies** are not supported.
//!   3. Connections using **bucket endpoint egress policies** are not supported.
//!   4. **Self-service private link** policies are generally not supported
//!      (operator-provisioned private links are OK).
//!   5. Connections using virtual tables require **direct egress policies**.
//!
//! This module fetches the source's worker_kind + egress.kind from
//! `connector-management-service` and rejects the registration if any
//! of the five rules above is violated. Each rejection is converted
//! to an HTTP 412 PRECONDITION FAILED with a stable error code so the
//! UI can show the user *exactly* which setting needs to change.

use std::time::Duration;

use serde::{Deserialize, Serialize};

use crate::AppState;

/// Worker that owns the source. Only `FoundryWorker` is compatible
/// with virtual tables (doc § "Limitations" rule 1).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum WorkerKind {
    FoundryWorker,
    AgentWorker,
}

impl WorkerKind {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::FoundryWorker => "FOUNDRY_WORKER",
            Self::AgentWorker => "AGENT_WORKER",
        }
    }
}

/// Egress policy kind. Only `Direct` and `PrivateLinkOperatorProvisioned`
/// are compatible with virtual tables (doc § "Limitations" rules 2-5).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum EgressKind {
    Direct,
    PrivateLinkOperatorProvisioned,
    SelfServicePrivateLink,
    AgentProxy,
    BucketEndpoint,
}

impl EgressKind {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Direct => "DIRECT",
            Self::PrivateLinkOperatorProvisioned => "PRIVATE_LINK_OPERATOR_PROVISIONED",
            Self::SelfServicePrivateLink => "SELF_SERVICE_PRIVATE_LINK",
            Self::AgentProxy => "AGENT_PROXY",
            Self::BucketEndpoint => "BUCKET_ENDPOINT",
        }
    }
}

/// Stable error codes surfaced in the 412 body so the UI / tests can
/// branch on the exact failure mode instead of parsing free text.
pub mod error_code {
    pub const SOURCE_NOT_FOUND: &str = "SOURCE_NOT_FOUND";
    pub const AGENT_WORKER_NOT_SUPPORTED: &str = "AGENT_WORKER_NOT_SUPPORTED";
    pub const AGENT_PROXY_EGRESS_NOT_SUPPORTED: &str = "AGENT_PROXY_EGRESS_NOT_SUPPORTED";
    pub const BUCKET_ENDPOINT_EGRESS_NOT_SUPPORTED: &str =
        "BUCKET_ENDPOINT_EGRESS_NOT_SUPPORTED";
    pub const SELF_SERVICE_PRIVATE_LINK_NOT_SUPPORTED: &str =
        "SELF_SERVICE_PRIVATE_LINK_NOT_SUPPORTED";
    pub const UPSTREAM_LOOKUP_FAILED: &str = "UPSTREAM_LOOKUP_FAILED";
}

/// Reason for rejecting a registration. Each variant carries the doc-aligned
/// remediation hint we surface to the user.
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
#[serde(tag = "code", rename_all = "SCREAMING_SNAKE_CASE")]
pub enum RejectionReason {
    SourceNotFound {
        source_rid: String,
    },
    AgentWorkerNotSupported {
        observed: String,
        remediation: &'static str,
    },
    AgentProxyEgressNotSupported {
        observed: String,
        remediation: &'static str,
    },
    BucketEndpointEgressNotSupported {
        observed: String,
        remediation: &'static str,
    },
    SelfServicePrivateLinkNotSupported {
        observed: String,
        remediation: &'static str,
    },
    UpstreamLookupFailed {
        endpoint: String,
        error: String,
    },
}

impl RejectionReason {
    pub fn code(&self) -> &'static str {
        match self {
            Self::SourceNotFound { .. } => error_code::SOURCE_NOT_FOUND,
            Self::AgentWorkerNotSupported { .. } => error_code::AGENT_WORKER_NOT_SUPPORTED,
            Self::AgentProxyEgressNotSupported { .. } => {
                error_code::AGENT_PROXY_EGRESS_NOT_SUPPORTED
            }
            Self::BucketEndpointEgressNotSupported { .. } => {
                error_code::BUCKET_ENDPOINT_EGRESS_NOT_SUPPORTED
            }
            Self::SelfServicePrivateLinkNotSupported { .. } => {
                error_code::SELF_SERVICE_PRIVATE_LINK_NOT_SUPPORTED
            }
            Self::UpstreamLookupFailed { .. } => error_code::UPSTREAM_LOOKUP_FAILED,
        }
    }

    /// Stable Prometheus label for the failure reason. Mirrors `code()`
    /// but lower-cased so the metric stays consistent with the other
    /// counters in this service (`record_validation_failure(reason: &str)`).
    pub fn metric_reason(&self) -> &'static str {
        match self {
            Self::SourceNotFound { .. } => "source_not_found",
            Self::AgentWorkerNotSupported { .. } => "agent_worker",
            Self::AgentProxyEgressNotSupported { .. } => "agent_proxy_egress",
            Self::BucketEndpointEgressNotSupported { .. } => "bucket_endpoint_egress",
            Self::SelfServicePrivateLinkNotSupported { .. } => "self_service_private_link",
            Self::UpstreamLookupFailed { .. } => "upstream_lookup_failed",
        }
    }
}

/// Subset of the connector-management-service `GET /v1/sources/{rid}`
/// response that we care about. The upstream returns a richer payload;
/// the `#[serde(default)]` on every field keeps us forward-compatible
/// with new keys (extra fields are simply ignored).
#[derive(Debug, Clone, Default, Deserialize)]
pub struct UpstreamSource {
    #[serde(default)]
    pub rid: String,
    #[serde(default)]
    pub provider: String,
    #[serde(default)]
    pub worker_kind: Option<WorkerKind>,
    #[serde(default)]
    pub egress: Option<UpstreamEgress>,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct UpstreamEgress {
    #[serde(default)]
    pub kind: Option<EgressKind>,
}

const REMEDIATE_AGENT_WORKER: &str =
    "Switch the source to a Foundry worker. Agent workers are not supported \
     for virtual tables (Foundry doc § Limitations).";
const REMEDIATE_AGENT_PROXY: &str =
    "Re-create the source with a direct egress policy. Agent proxy policies \
     are not supported for virtual tables.";
const REMEDIATE_BUCKET_ENDPOINT: &str =
    "Re-create the source with a direct egress policy. Bucket-endpoint \
     policies are not supported for virtual tables.";
const REMEDIATE_PRIVATE_LINK: &str =
    "Self-service private-link policies are not supported. Either use a \
     direct egress policy or request an operator-provisioned private link \
     from your Palantir representative.";

/// Apply the five doc-aligned rules to a fully-loaded `UpstreamSource`.
///
/// Returning `Ok(())` means the source is compatible with virtual
/// tables; an `Err(RejectionReason)` carries the exact rule that was
/// violated plus a remediation hint.
pub fn validate_source_compatibility(source: &UpstreamSource) -> Result<(), RejectionReason> {
    // Rule 1: agent-worker sources are blocked outright.
    if matches!(source.worker_kind, Some(WorkerKind::AgentWorker)) {
        return Err(RejectionReason::AgentWorkerNotSupported {
            observed: WorkerKind::AgentWorker.as_str().into(),
            remediation: REMEDIATE_AGENT_WORKER,
        });
    }

    // Rules 2-5: only DIRECT and operator-provisioned private link are OK.
    let kind = source
        .egress
        .as_ref()
        .and_then(|e| e.kind)
        // Default to DIRECT when the upstream omits the field — the
        // most permissive interpretation matches what Foundry exposes
        // for legacy sources.
        .unwrap_or(EgressKind::Direct);

    match kind {
        EgressKind::Direct | EgressKind::PrivateLinkOperatorProvisioned => Ok(()),
        EgressKind::AgentProxy => Err(RejectionReason::AgentProxyEgressNotSupported {
            observed: kind.as_str().into(),
            remediation: REMEDIATE_AGENT_PROXY,
        }),
        EgressKind::BucketEndpoint => Err(RejectionReason::BucketEndpointEgressNotSupported {
            observed: kind.as_str().into(),
            remediation: REMEDIATE_BUCKET_ENDPOINT,
        }),
        EgressKind::SelfServicePrivateLink => {
            Err(RejectionReason::SelfServicePrivateLinkNotSupported {
                observed: kind.as_str().into(),
                remediation: REMEDIATE_PRIVATE_LINK,
            })
        }
    }
}

/// Top-level entry point for the registration path.
///
/// In strict mode (`AppState::strict_source_validation = true`, the
/// production default) we hit `connector-management-service` to
/// resolve the source and run [`validate_source_compatibility`]. When
/// strict mode is disabled (integration tests) the call returns
/// `Ok(())` immediately.
pub async fn validate_for_virtual_tables(
    state: &AppState,
    source_rid: &str,
) -> Result<UpstreamSource, RejectionReason> {
    if !state.strict_source_validation {
        return Ok(UpstreamSource {
            rid: source_rid.to_string(),
            ..UpstreamSource::default()
        });
    }

    let endpoint = format!(
        "{}/api/v1/data-connection/sources/{}",
        state
            .connector_management_service_url
            .trim_end_matches('/'),
        source_rid
    );

    let response = state
        .http_client
        .get(&endpoint)
        .timeout(Duration::from_secs(5))
        .send()
        .await
        .map_err(|error| RejectionReason::UpstreamLookupFailed {
            endpoint: endpoint.clone(),
            error: error.to_string(),
        })?;

    if response.status() == reqwest::StatusCode::NOT_FOUND {
        return Err(RejectionReason::SourceNotFound {
            source_rid: source_rid.to_string(),
        });
    }
    if !response.status().is_success() {
        return Err(RejectionReason::UpstreamLookupFailed {
            endpoint,
            error: format!("HTTP {}", response.status()),
        });
    }

    let source: UpstreamSource =
        response
            .json()
            .await
            .map_err(|error| RejectionReason::UpstreamLookupFailed {
                endpoint: endpoint.clone(),
                error: error.to_string(),
            })?;

    validate_source_compatibility(&source).map(|_| source)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn source_with(worker: WorkerKind, egress: EgressKind) -> UpstreamSource {
        UpstreamSource {
            rid: "ri.foundry.main.source.123".into(),
            provider: "BIGQUERY".into(),
            worker_kind: Some(worker),
            egress: Some(UpstreamEgress { kind: Some(egress) }),
        }
    }

    #[test]
    fn foundry_worker_with_direct_egress_is_accepted() {
        let s = source_with(WorkerKind::FoundryWorker, EgressKind::Direct);
        assert!(validate_source_compatibility(&s).is_ok());
    }

    #[test]
    fn foundry_worker_with_operator_private_link_is_accepted() {
        let s = source_with(
            WorkerKind::FoundryWorker,
            EgressKind::PrivateLinkOperatorProvisioned,
        );
        assert!(validate_source_compatibility(&s).is_ok());
    }

    #[test]
    fn agent_worker_is_rejected_regardless_of_egress() {
        for egress in [
            EgressKind::Direct,
            EgressKind::PrivateLinkOperatorProvisioned,
        ] {
            let s = source_with(WorkerKind::AgentWorker, egress);
            let err = validate_source_compatibility(&s).expect_err("must reject");
            assert_eq!(err.code(), error_code::AGENT_WORKER_NOT_SUPPORTED);
        }
    }

    #[test]
    fn agent_proxy_egress_is_rejected() {
        let s = source_with(WorkerKind::FoundryWorker, EgressKind::AgentProxy);
        let err = validate_source_compatibility(&s).expect_err("must reject");
        assert_eq!(err.code(), error_code::AGENT_PROXY_EGRESS_NOT_SUPPORTED);
    }

    #[test]
    fn bucket_endpoint_egress_is_rejected() {
        let s = source_with(WorkerKind::FoundryWorker, EgressKind::BucketEndpoint);
        let err = validate_source_compatibility(&s).expect_err("must reject");
        assert_eq!(err.code(), error_code::BUCKET_ENDPOINT_EGRESS_NOT_SUPPORTED);
    }

    #[test]
    fn self_service_private_link_is_rejected() {
        let s = source_with(
            WorkerKind::FoundryWorker,
            EgressKind::SelfServicePrivateLink,
        );
        let err = validate_source_compatibility(&s).expect_err("must reject");
        assert_eq!(err.code(), error_code::SELF_SERVICE_PRIVATE_LINK_NOT_SUPPORTED);
    }

    #[test]
    fn missing_egress_field_defaults_to_direct() {
        let s = UpstreamSource {
            rid: "x".into(),
            provider: "S3".into(),
            worker_kind: Some(WorkerKind::FoundryWorker),
            egress: None,
        };
        assert!(validate_source_compatibility(&s).is_ok());
    }

    #[test]
    fn rejection_reason_metric_labels_are_lowercase_snake() {
        for reason in [
            RejectionReason::SourceNotFound { source_rid: "x".into() },
            RejectionReason::AgentWorkerNotSupported {
                observed: "AGENT_WORKER".into(),
                remediation: REMEDIATE_AGENT_WORKER,
            },
            RejectionReason::AgentProxyEgressNotSupported {
                observed: "AGENT_PROXY".into(),
                remediation: REMEDIATE_AGENT_PROXY,
            },
            RejectionReason::BucketEndpointEgressNotSupported {
                observed: "BUCKET_ENDPOINT".into(),
                remediation: REMEDIATE_BUCKET_ENDPOINT,
            },
            RejectionReason::SelfServicePrivateLinkNotSupported {
                observed: "SELF_SERVICE_PRIVATE_LINK".into(),
                remediation: REMEDIATE_PRIVATE_LINK,
            },
            RejectionReason::UpstreamLookupFailed {
                endpoint: "x".into(),
                error: "y".into(),
            },
        ] {
            let label = reason.metric_reason();
            assert!(label.chars().all(|c| c.is_ascii_lowercase() || c == '_'));
        }
    }
}
