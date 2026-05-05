//! Doc-conformance test for Foundry-worker / egress enforcement.
//!
//! Re-encodes the five "not supported" rules from the Foundry doc §
//! "Limitations of using virtual tables" as a test matrix and asserts
//! that `validate_source_compatibility` rejects each disallowed
//! configuration with a stable error code.

use virtual_table_service::domain::source_validation::{
    EgressKind, RejectionReason, UpstreamEgress, UpstreamSource, WorkerKind, error_code,
    validate_source_compatibility,
};

fn source(worker: WorkerKind, egress: EgressKind) -> UpstreamSource {
    UpstreamSource {
        rid: "ri.foundry.main.source.test".into(),
        provider: "BIGQUERY".into(),
        worker_kind: Some(worker),
        egress: Some(UpstreamEgress { kind: Some(egress) }),
    }
}

#[test]
fn foundry_worker_with_direct_or_operator_private_link_is_accepted() {
    for egress in [
        EgressKind::Direct,
        EgressKind::PrivateLinkOperatorProvisioned,
    ] {
        let s = source(WorkerKind::FoundryWorker, egress);
        validate_source_compatibility(&s).expect("must accept");
    }
}

#[test]
fn agent_worker_is_rejected_for_every_egress_kind() {
    for egress in [
        EgressKind::Direct,
        EgressKind::PrivateLinkOperatorProvisioned,
        EgressKind::AgentProxy,
        EgressKind::BucketEndpoint,
        EgressKind::SelfServicePrivateLink,
    ] {
        let s = source(WorkerKind::AgentWorker, egress);
        let err = validate_source_compatibility(&s).expect_err("must reject");
        assert_eq!(err.code(), error_code::AGENT_WORKER_NOT_SUPPORTED);
    }
}

#[test]
fn agent_proxy_egress_is_rejected_with_remediation() {
    let s = source(WorkerKind::FoundryWorker, EgressKind::AgentProxy);
    let err = validate_source_compatibility(&s).expect_err("must reject");
    assert_eq!(err.code(), error_code::AGENT_PROXY_EGRESS_NOT_SUPPORTED);
    if let RejectionReason::AgentProxyEgressNotSupported { remediation, .. } = err {
        assert!(remediation.to_lowercase().contains("direct"));
    } else {
        panic!("unexpected variant");
    }
}

#[test]
fn bucket_endpoint_egress_is_rejected() {
    let s = source(WorkerKind::FoundryWorker, EgressKind::BucketEndpoint);
    let err = validate_source_compatibility(&s).expect_err("must reject");
    assert_eq!(err.code(), error_code::BUCKET_ENDPOINT_EGRESS_NOT_SUPPORTED);
}

#[test]
fn self_service_private_link_is_rejected() {
    let s = source(
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
    validate_source_compatibility(&s).expect("default-direct must be accepted");
}

#[test]
fn metric_reasons_are_lowercase_snake() {
    let reasons = [
        RejectionReason::AgentWorkerNotSupported {
            observed: "AGENT_WORKER".into(),
            remediation: "x",
        },
        RejectionReason::AgentProxyEgressNotSupported {
            observed: "AGENT_PROXY".into(),
            remediation: "x",
        },
        RejectionReason::BucketEndpointEgressNotSupported {
            observed: "BUCKET_ENDPOINT".into(),
            remediation: "x",
        },
        RejectionReason::SelfServicePrivateLinkNotSupported {
            observed: "SELF_SERVICE_PRIVATE_LINK".into(),
            remediation: "x",
        },
    ];
    for reason in reasons {
        let label = reason.metric_reason();
        assert!(
            label.chars().all(|c| c.is_ascii_lowercase() || c == '_'),
            "label '{}' is not lowercase_snake",
            label
        );
    }
}
