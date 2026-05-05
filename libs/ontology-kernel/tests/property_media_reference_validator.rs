//! H6 — integration coverage for `domain::media_reference_validator`.
//!
//! The unit tests inside `media_reference_validator.rs` exercise each
//! shape error in isolation; this file pins the **integration**
//! contract every action handler relies on:
//!
//!   1. Happy path round-trips a Foundry payload (camelCase keys
//!      from the OSDK) and surfaces the parsed reference back to the
//!      caller — handlers persist the parsed form.
//!   2. The clearance check honours an empty marking set (any user
//!      can edit a public set) and rejects when even one marking is
//!      missing from the user's clearances.
//!   3. The set-existence check fires before the clearance check —
//!      handlers translate it to 404 vs 403 based on this ordering.
//!
//! `tests/*.rs` lives in a separate crate target than `src/`, so we
//! exercise the same public surface a handler would import.

use ontology_kernel::domain::media_reference_validator::{
    MediaReferenceValidationError, ResolvedMediaSet, context_from_map, validate,
};
use serde_json::json;
use std::collections::HashMap;

fn ctx_with(
    set_rid: &str,
    markings: &[&str],
    clearances: &[&str],
) -> ontology_kernel::domain::media_reference_validator::MediaReferenceContext<'static> {
    let mut sets = HashMap::new();
    sets.insert(
        set_rid.to_string(),
        ResolvedMediaSet {
            media_set_rid: set_rid.to_string(),
            markings: markings.iter().map(|s| s.to_string()).collect(),
        },
    );
    context_from_map(
        sets,
        clearances.iter().map(|s| s.to_string()).collect(),
    )
}

#[test]
fn happy_path_round_trips_foundry_payload() {
    let payload = json!({
        "mediaSetRid": "ri.foundry.main.media_set.aircraft-photos",
        "mediaItemRid": "ri.foundry.main.media_item.f-16-001",
        "branch": "main",
        "schema": "IMAGE"
    });
    let ctx = ctx_with(
        "ri.foundry.main.media_set.aircraft-photos",
        &["public"],
        &["public"],
    );
    let parsed = validate(&payload, &ctx).expect("happy path must validate");
    assert_eq!(parsed.media_set_rid, "ri.foundry.main.media_set.aircraft-photos");
    assert_eq!(parsed.media_item_rid, "ri.foundry.main.media_item.f-16-001");
    assert_eq!(parsed.branch.as_deref(), Some("main"));
    assert_eq!(parsed.schema.as_deref(), Some("IMAGE"));
}

#[test]
fn empty_markings_means_any_user_can_edit() {
    let payload = json!({
        "mediaSetRid": "ri.foundry.main.media_set.public-set",
        "mediaItemRid": "x"
    });
    let ctx = ctx_with("ri.foundry.main.media_set.public-set", &[], &[]);
    validate(&payload, &ctx).expect("empty markings → any user passes");
}

#[test]
fn missing_one_marking_blocks_the_edit() {
    let payload = json!({
        "mediaSetRid": "ri.foundry.main.media_set.classified",
        "mediaItemRid": "x"
    });
    let ctx = ctx_with(
        "ri.foundry.main.media_set.classified",
        &["public", "secret"],
        &["public"],
    );
    let err = validate(&payload, &ctx).expect_err("missing clearance must fail");
    match err {
        MediaReferenceValidationError::InsufficientClearance { missing } => {
            assert_eq!(missing, "secret");
        }
        other => panic!("expected InsufficientClearance, got {other:?}"),
    }
}

#[test]
fn missing_set_fires_before_clearance_check() {
    // Caller has zero clearances. If the order were inverted the
    // error would be `InsufficientClearance` (no clearances cover
    // the set's markings) — but the set itself is missing, so we
    // must surface the dangling-pointer error first.
    let payload = json!({
        "mediaSetRid": "ri.foundry.main.media_set.never-was",
        "mediaItemRid": "x"
    });
    let ctx = ctx_with(
        "ri.foundry.main.media_set.exists-but-different",
        &["public"],
        &[],
    );
    let err = validate(&payload, &ctx).unwrap_err();
    assert!(
        matches!(err, MediaReferenceValidationError::UnknownMediaSet(_)),
        "expected UnknownMediaSet first, got {err:?}",
    );
}
