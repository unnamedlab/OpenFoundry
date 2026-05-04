//! Streaming profiles — deterministic composition.
//!
//! Mirrors the Foundry docs' "profiles can be composed with each other
//! to meet your use case requirements" sentence and the Pipeline
//! Builder "Advanced" preview shown in `Streaming profiles_assets/img_002.png`.
//! The resolver in [`compose_effective_config`] sorts profiles by
//! category specificity then attached order, so a fresh import or
//! re-attach with the same set yields the same effective config every
//! time.

use chrono::Utc;
use event_streaming_service::models::profile::{
    EffectiveFlinkConfig, ProfileCategory, ProfileSizeClass, StreamingProfile,
    compose_effective_config,
};
use serde_json::json;
use uuid::Uuid;

fn p(name: &str, category: ProfileCategory, cfg: serde_json::Value) -> StreamingProfile {
    StreamingProfile {
        id: Uuid::now_v7(),
        name: name.to_string(),
        description: String::new(),
        category,
        size_class: ProfileSizeClass::Small,
        restricted: false,
        config_json: cfg,
        version: 1,
        created_by: "tests".into(),
        created_at: Utc::now(),
        updated_at: Utc::now(),
    }
}

#[test]
fn profiles_composition_is_stable_under_input_permutation() {
    let a = p(
        "tm",
        ProfileCategory::TaskmanagerResources,
        json!({"taskmanager.numberOfTaskSlots": "4"}),
    );
    let b = p(
        "par",
        ProfileCategory::Parallelism,
        json!({"parallelism.default": "8"}),
    );
    let c = p(
        "net",
        ProfileCategory::Network,
        json!({"taskmanager.network.memory.fraction": "0.2"}),
    );

    let r1 = compose_effective_config("p", &[(a.clone(), 1), (b.clone(), 2), (c.clone(), 3)]);
    let r2 = compose_effective_config("p", &[(c.clone(), 3), (a.clone(), 1), (b.clone(), 2)]);
    assert_eq!(r1.config, r2.config);
    // Source map must agree too — that's what the UI shows in the
    // Advanced panel.
    assert_eq!(r1.source_map, r2.source_map);
}

#[test]
fn profiles_last_imported_wins_on_conflict_within_same_category() {
    let earlier = p(
        "earlier",
        ProfileCategory::Parallelism,
        json!({"parallelism.default": "4"}),
    );
    let later = p(
        "later",
        ProfileCategory::Parallelism,
        json!({"parallelism.default": "16"}),
    );

    // Larger `attached_order` is the "last imported" tie-breaker.
    let r = compose_effective_config("p", &[(earlier.clone(), 1), (later.clone(), 5)]);
    assert_eq!(
        r.config["parallelism.default"],
        serde_json::Value::String("16".into())
    );
    assert!(
        r.warnings.iter().any(|w| w.contains("parallelism.default")),
        "override must surface a warning"
    );
}

#[test]
fn profiles_advanced_category_runs_last_so_explicit_override_wins() {
    // ADVANCED is the least specific category — a follow-up Advanced
    // profile is meant to be an "escape hatch" override on top of a
    // category-specific stack.
    let parallelism = p(
        "par",
        ProfileCategory::Parallelism,
        json!({"parallelism.default": "8"}),
    );
    let advanced = p(
        "adv",
        ProfileCategory::Advanced,
        json!({"parallelism.default": "32", "state.backend.type": "rocksdb"}),
    );
    let r = compose_effective_config("p", &[(parallelism, 1), (advanced.clone(), 2)]);
    assert_eq!(
        r.config["parallelism.default"],
        serde_json::Value::String("32".into())
    );
    assert_eq!(
        r.config["state.backend.type"],
        serde_json::Value::String("rocksdb".into())
    );
}

#[test]
fn profiles_non_overlapping_keys_compose_into_a_union() {
    let mem = p(
        "mem",
        ProfileCategory::TaskmanagerResources,
        json!({"taskmanager.memory.process.size": "4g"}),
    );
    let net = p(
        "net",
        ProfileCategory::Network,
        json!({"taskmanager.network.memory.fraction": "0.2"}),
    );
    let r: EffectiveFlinkConfig = compose_effective_config("p", &[(mem, 1), (net, 2)]);
    assert_eq!(r.config.as_object().unwrap().len(), 2);
    assert!(r.warnings.is_empty(), "no overlap → no warnings");
}
