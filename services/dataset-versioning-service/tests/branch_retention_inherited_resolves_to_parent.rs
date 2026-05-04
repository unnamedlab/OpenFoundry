//! P4 — `INHERITED` resolves up the parent chain.
//!
//! Pure-Rust unit test against the in-memory resolver — no Docker.
//! Replicates the doc § "Branch retention" sentence "branches inherit
//! retention from their parent". A child with `INHERITED` and a
//! grandparent with `FOREVER` is never archive-eligible, even though
//! the child's `last_activity_at` is ancient.

use std::collections::HashMap;

use chrono::{Duration, TimeZone, Utc};
use dataset_versioning_service::domain::retention::{
    EffectiveRetention, RetentionPolicy, RetentionRow, is_archive_eligible,
    resolve_effective_retention,
};
use uuid::Uuid;

fn row(id: u128, parent: Option<u128>, policy: RetentionPolicy, ttl: Option<i32>) -> RetentionRow {
    RetentionRow {
        id: Uuid::from_u128(id),
        parent_branch_id: parent.map(Uuid::from_u128),
        policy,
        ttl_days: ttl,
        last_activity_at: Utc.with_ymd_and_hms(2024, 1, 1, 0, 0, 0).unwrap(),
        has_open_transaction: false,
        is_root: parent.is_none(),
        archived_at: None,
    }
}

#[test]
fn inherited_chain_to_forever_makes_child_ineligible() {
    let master = row(1, None, RetentionPolicy::Forever, None);
    let develop = row(2, Some(1), RetentionPolicy::Inherited, None);
    let feature = row(3, Some(2), RetentionPolicy::Inherited, None);
    let index: HashMap<Uuid, RetentionRow> = vec![master.clone(), develop.clone(), feature.clone()]
        .into_iter()
        .map(|r| (r.id, r))
        .collect();

    let eff = resolve_effective_retention(&feature, &index);
    assert_eq!(eff.policy, RetentionPolicy::Forever);
    assert_eq!(eff.source_branch_id, Some(master.id));

    // Despite `last_activity_at = 2024`, the FOREVER ancestor pins
    // the feature branch out of the archive queue.
    assert!(!is_archive_eligible(&feature, &eff, Utc::now()));
}

#[test]
fn inherited_chain_to_ttl_inherits_window_from_grandparent() {
    let master = row(1, None, RetentionPolicy::TtlDays, Some(7));
    let develop = row(2, Some(1), RetentionPolicy::Inherited, None);
    let mut feature = row(3, Some(2), RetentionPolicy::Inherited, None);
    // Make feature stale enough to trip the inherited 7-day TTL.
    feature.last_activity_at = Utc::now() - Duration::days(10);

    let index: HashMap<Uuid, RetentionRow> = vec![master.clone(), develop.clone(), feature.clone()]
        .into_iter()
        .map(|r| (r.id, r))
        .collect();
    let eff = resolve_effective_retention(&feature, &index);
    assert_eq!(eff.policy, RetentionPolicy::TtlDays);
    assert_eq!(eff.ttl_days, Some(7));
    assert!(is_archive_eligible(&feature, &eff, Utc::now()));
}

#[test]
fn explicit_forever_on_child_overrides_parent_ttl() {
    let master = row(1, None, RetentionPolicy::TtlDays, Some(1));
    let mut feature = row(2, Some(1), RetentionPolicy::Forever, None);
    feature.last_activity_at = Utc.with_ymd_and_hms(2020, 1, 1, 0, 0, 0).unwrap();
    let index: HashMap<Uuid, RetentionRow> = vec![master, feature.clone()]
        .into_iter()
        .map(|r| (r.id, r))
        .collect();
    let eff = resolve_effective_retention(&feature, &index);
    assert_eq!(eff.policy, RetentionPolicy::Forever);
    let _ = EffectiveRetention::default_forever();
    assert!(!is_archive_eligible(&feature, &eff, Utc::now()));
}
