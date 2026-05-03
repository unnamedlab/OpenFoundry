//! Streaming profiles — CRUD authorization rules.
//!
//! Foundry's docs are explicit: profile lifecycle (create/update,
//! restrict/unrestrict) lives behind admin / `streaming_admin` /
//! explicit `streaming:profile-write`, while project-import flows
//! through `compass:import-resource-to`. This test pins the constants
//! the handler keys off of so a future refactor can't accidentally
//! widen the surface.

use event_streaming_service::handlers::profiles::{
    ERR_PROFILE_INVALID_KEY, ERR_PROFILE_NOT_IMPORTED,
    ERR_PROFILE_RESTRICTED_REQUIRES_ADMIN,
};
use event_streaming_service::models::profile::{
    CreateProfileRequest, FLINK_CONFIG_KEY_WHITELIST, ProfileCategory, ProfileSizeClass,
    validate_config_keys,
};
use serde_json::json;

#[test]
fn profiles_error_codes_match_documented_constants() {
    assert_eq!(
        ERR_PROFILE_RESTRICTED_REQUIRES_ADMIN,
        "STREAMING_PROFILE_RESTRICTED_REQUIRES_ENROLLMENT_ADMIN"
    );
    assert_eq!(ERR_PROFILE_NOT_IMPORTED, "STREAMING_PROFILE_NOT_IMPORTED");
    assert_eq!(
        ERR_PROFILE_INVALID_KEY,
        "STREAMING_PROFILE_INVALID_FLINK_KEY"
    );
}

#[test]
fn profiles_create_request_round_trips() {
    let req: CreateProfileRequest = serde_json::from_value(json!({
        "name": "Custom",
        "category": "TASKMANAGER_RESOURCES",
        "size_class": "MEDIUM",
        "config_json": { "taskmanager.numberOfTaskSlots": "4" }
    }))
    .unwrap();
    assert_eq!(req.name, "Custom");
    assert!(matches!(req.category, ProfileCategory::TaskmanagerResources));
    assert!(matches!(req.size_class, ProfileSizeClass::Medium));
    // `restricted` defaults to None so the handler can apply the
    // LARGE-defaults-to-restricted rule.
    assert_eq!(req.restricted, None);
}

#[test]
fn profiles_whitelist_includes_documented_keys() {
    // Lock in the exact whitelist surface so a future shrink that
    // breaks Foundry parity must update this test.
    for required in [
        "taskmanager.memory.process.size",
        "taskmanager.numberOfTaskSlots",
        "parallelism.default",
        "state.backend.type",
        "execution.checkpointing.interval",
        "taskmanager.network.memory.fraction",
    ] {
        assert!(
            FLINK_CONFIG_KEY_WHITELIST.contains(&required),
            "whitelist must include {required}"
        );
    }
}

#[test]
fn profiles_validate_config_keys_rejects_escape_hatches() {
    let cfg = json!({
        "execution.runtime-mode": "BATCH",
        "taskmanager.numberOfTaskSlots": "4"
    });
    let err = validate_config_keys(&cfg).unwrap_err();
    assert!(err.iter().any(|e| e.contains("execution.runtime-mode")));
}
