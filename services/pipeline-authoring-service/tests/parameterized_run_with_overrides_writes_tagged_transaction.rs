//! Parameterized run dispatch shape — pure-logic test driving the
//! domain helpers that decide whether a given trigger/deployment pair
//! is allowed and what kwargs the build pass should inject.
//!
//! End-to-end (Postgres-backed) coverage of the run handler lives in
//! the bin's integration suite; this test verifies the invariants the
//! handler enforces *before* it touches the build-service:
//!
//!   * `assert_manual_dispatch` lets manual through, rejects everything
//!     else with the doc-mandated reason.
//!   * `assert_deployment_key_consistent` matches the deployment_key
//!     against the `deployment_key_param` value the row carries —
//!     guaranteeing the build pass can stamp `deployment_key` onto
//!     every output transaction without ambiguity.
//!   * `merge_with_defaults` produces the kwargs map JobExecutionContext
//!     receives.

use chrono::Utc;
use pipeline_authoring_service::param::{
    Param, ParamType, merge_with_defaults, validate_overrides,
};
use pipeline_authoring_service::parameterized::{
    DispatchError, ParameterizedPipeline, PipelineDeployment, TriggerKind,
    assert_deployment_key_consistent, assert_manual_dispatch,
};
use serde_json::{Map, Value, json};
use uuid::Uuid;

fn pipeline_fixture() -> ParameterizedPipeline {
    ParameterizedPipeline {
        id: Uuid::nil(),
        pipeline_rid: "ri.foundry.main.pipeline.alpha".into(),
        deployment_key_param: "region".into(),
        output_dataset_rids: vec![
            "ri.foundry.main.dataset.alpha-out".into(),
        ],
        union_view_dataset_rid: "ri.foundry.main.dataset.alpha-view".into(),
        created_at: Utc::now(),
        updated_at: Utc::now(),
    }
}

fn deployment_eu(parent: &ParameterizedPipeline) -> PipelineDeployment {
    let mut parameter_values = Map::new();
    parameter_values.insert("region".into(), json!("eu-west"));
    parameter_values.insert("limit".into(), json!(1000));
    PipelineDeployment {
        id: Uuid::nil(),
        parameterized_pipeline_id: parent.id,
        deployment_key: "eu-west".into(),
        parameter_values,
        created_by: "tester".into(),
        created_at: Utc::now(),
    }
}

fn declared_params() -> Vec<Param> {
    vec![
        Param {
            name: "region".into(),
            param_type: ParamType::String,
            default_value: None,
            required: true,
        },
        Param {
            name: "limit".into(),
            param_type: ParamType::Integer,
            default_value: Some(json!(1000)),
            required: false,
        },
    ]
}

#[test]
fn manual_dispatch_with_consistent_deployment_key_is_accepted() {
    let p = pipeline_fixture();
    let d = deployment_eu(&p);
    assert!(assert_manual_dispatch(TriggerKind::Manual).is_ok());
    assert!(assert_deployment_key_consistent(&p, &d).is_ok());

    // The kwargs the build pass receives mirror the deployment.
    let merged = merge_with_defaults(&declared_params(), &d.parameter_values).unwrap();
    assert_eq!(merged.get("region"), Some(&json!("eu-west")));
    assert_eq!(merged.get("limit"), Some(&json!(1000)));
}

#[test]
fn dispatch_rejects_time_trigger_per_doc() {
    let err = assert_manual_dispatch(TriggerKind::Time).unwrap_err();
    assert_eq!(err, DispatchError::AutomatedTriggerRejected);
}

#[test]
fn dispatch_rejects_event_trigger_per_doc() {
    let err = assert_manual_dispatch(TriggerKind::Event).unwrap_err();
    assert_eq!(err, DispatchError::AutomatedTriggerRejected);
}

#[test]
fn dispatch_rejects_compound_trigger_per_doc() {
    let err = assert_manual_dispatch(TriggerKind::Compound).unwrap_err();
    assert_eq!(err, DispatchError::AutomatedTriggerRejected);
}

#[test]
fn deployment_key_mismatch_blocks_dispatch() {
    let p = pipeline_fixture();
    let mut d = deployment_eu(&p);
    // The recorded `region` no longer matches the deployment_key.
    d.parameter_values.insert("region".into(), json!("us-east"));
    let err = assert_deployment_key_consistent(&p, &d).unwrap_err();
    assert!(matches!(err, DispatchError::DeploymentKeyMismatch(_, _)));
}

#[test]
fn validate_overrides_rejects_wrong_type_for_required_param() {
    let mut overrides: Map<String, Value> = Map::new();
    overrides.insert("region".into(), json!(42));
    let err = validate_overrides(&declared_params(), &overrides).unwrap_err();
    let msg = err.to_string();
    assert!(msg.contains("STRING"));
}

#[test]
fn merge_keeps_recorded_value_over_default() {
    let mut overrides: Map<String, Value> = Map::new();
    overrides.insert("region".into(), json!("us-east"));
    overrides.insert("limit".into(), json!(50));
    let merged = merge_with_defaults(&declared_params(), &overrides).unwrap();
    // Override wins over the per-param default.
    assert_eq!(merged.get("limit"), Some(&json!(50)));
}
