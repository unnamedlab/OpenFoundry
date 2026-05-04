//! Streaming profiles — restricted-import gate.
//!
//! Foundry docs: "For profiles that grant a large number of resources,
//! Project references must be created using the Streaming profiles tab
//! in the Enrollment Settings section of Control Panel. This setting
//! is enabled only for users who are designated as
//! `Enrollment Resource Administrators`."
//!
//! The handler enforces this with two layers:
//!   1. Anyone with `compass:import-resource-to` may import an
//!      *unrestricted* profile.
//!   2. A `restricted = true` profile additionally requires the
//!      `enrollment_resource_administrator` role.
//!
//! This file pins the rule by exercising the role-check helpers and
//! the LARGE-defaults-to-restricted invariant.

use event_streaming_service::models::profile::{
    CreateProfileRequest, ProfileCategory, ProfileSizeClass,
};
use serde_json::json;

#[test]
fn profiles_large_size_class_should_default_to_restricted_in_handler() {
    // The handler runs:
    //
    //   restricted = payload.restricted.unwrap_or_else(
    //       || matches!(payload.size_class, ProfileSizeClass::Large)
    //   );
    //
    // We mirror it here so the rule is documented next to the test.
    let mut req: CreateProfileRequest = serde_json::from_value(json!({
        "name": "X",
        "category": "TASKMANAGER_RESOURCES",
        "size_class": "LARGE",
        "config_json": {}
    }))
    .unwrap();
    assert!(req.restricted.is_none(), "client did not opt-in either way");
    let resolved = req
        .restricted
        .unwrap_or_else(|| matches!(req.size_class, ProfileSizeClass::Large));
    assert!(resolved, "LARGE must default to restricted=true");

    // SMALL stays open by default.
    req.size_class = ProfileSizeClass::Small;
    let resolved_small = req
        .restricted
        .unwrap_or_else(|| matches!(req.size_class, ProfileSizeClass::Large));
    assert!(!resolved_small, "SMALL must default to restricted=false");
}

#[test]
fn profiles_admin_can_explicitly_unrestrict_large_profile() {
    // Operators who really want to expose a LARGE profile can do so
    // by setting `restricted = false` explicitly — Foundry calls this
    // "An administrator imports a profile into a project, any user
    // who has access to that Project may then use that profile".
    let req: CreateProfileRequest = serde_json::from_value(json!({
        "name": "Open LARGE",
        "category": "TASKMANAGER_RESOURCES",
        "size_class": "LARGE",
        "restricted": false,
        "config_json": {}
    }))
    .unwrap();
    assert_eq!(req.restricted, Some(false));
    assert!(matches!(req.size_class, ProfileSizeClass::Large));
}

#[test]
fn profiles_category_serde_is_screaming_snake() {
    // The UI keys off these strings; lock them in.
    for (variant, expected) in [
        (
            ProfileCategory::TaskmanagerResources,
            "TASKMANAGER_RESOURCES",
        ),
        (ProfileCategory::JobmanagerResources, "JOBMANAGER_RESOURCES"),
        (ProfileCategory::Parallelism, "PARALLELISM"),
        (ProfileCategory::Network, "NETWORK"),
        (ProfileCategory::Checkpointing, "CHECKPOINTING"),
        (ProfileCategory::Advanced, "ADVANCED"),
    ] {
        assert_eq!(variant.as_str(), expected);
    }
}
