//! Streaming profiles — project-ref precondition for pipeline attach.
//!
//! Foundry docs: "If you try to use a streaming profile that is not
//! imported to your Project, your job will fail with an error
//! indicating this missing requirement." We surface that as a
//! 412 PRECONDITION_FAILED with the documented `ERR_PROFILE_NOT_IMPORTED`
//! code so the UI can render an actionable message.

use event_streaming_service::handlers::profiles::ERR_PROFILE_NOT_IMPORTED;
use event_streaming_service::models::profile::AttachProfileRequest;
use serde_json::json;

#[test]
fn profiles_error_code_matches_documented_constant() {
    assert_eq!(ERR_PROFILE_NOT_IMPORTED, "STREAMING_PROFILE_NOT_IMPORTED");
}

#[test]
fn profiles_attach_request_carries_project_rid() {
    // The attach payload must surface the project_rid so the handler
    // can confirm a project ref exists before inserting into
    // streaming_pipeline_profiles.
    let req: AttachProfileRequest = serde_json::from_value(json!({
        "project_rid": "ri.compass.main.project.foo",
        "profile_id": "01968040-0850-7920-9000-00000000aaa1"
    }))
    .unwrap();
    assert_eq!(req.project_rid, "ri.compass.main.project.foo");
}

#[test]
fn profiles_attach_request_rejects_missing_project_rid() {
    // Without `project_rid` the deserialiser must error out so the
    // handler does not silently fall through to the
    // streaming_pipeline_profiles INSERT (which would attach a
    // profile that is not actually imported anywhere).
    let res = serde_json::from_value::<AttachProfileRequest>(json!({
        "profile_id": "01968040-0850-7920-9000-00000000aaa1"
    }));
    assert!(res.is_err(), "missing project_rid must be rejected");
}
