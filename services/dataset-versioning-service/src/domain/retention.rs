//! Branch retention — Foundry "Branch retention" doc.
//!
//! Implements the three policies used by the archive worker:
//!
//!   * `FOREVER`  — never archived. Used by `master` and any
//!                  user-protected long-lived branch.
//!   * `TTL_DAYS` — archived when `last_activity_at` is older than
//!                  `ttl_days`, the branch has no OPEN transaction,
//!                  and is not a root.
//!   * `INHERITED` — walks up `parent_branch_id` until it finds a
//!                  branch with `FOREVER` or `TTL_DAYS`. The default
//!                  for new branches.
//!
//! The resolver is **pure** — it operates on already-loaded
//! `RetentionRow` records — so the unit tests in this module exercise
//! every edge case without a database.

use std::collections::HashMap;

use chrono::{DateTime, Duration, Utc};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum RetentionPolicy {
    Inherited,
    Forever,
    #[serde(rename = "TTL_DAYS")]
    TtlDays,
}

impl RetentionPolicy {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Inherited => "INHERITED",
            Self::Forever => "FOREVER",
            Self::TtlDays => "TTL_DAYS",
        }
    }

    pub fn parse(value: &str) -> Option<Self> {
        match value {
            "INHERITED" => Some(Self::Inherited),
            "FOREVER" => Some(Self::Forever),
            "TTL_DAYS" => Some(Self::TtlDays),
            _ => None,
        }
    }
}

/// Just enough of a row to resolve `INHERITED` against the parent
/// chain. Loaded in bulk by the worker and re-used for the resolver.
#[derive(Debug, Clone)]
pub struct RetentionRow {
    pub id: Uuid,
    pub parent_branch_id: Option<Uuid>,
    pub policy: RetentionPolicy,
    pub ttl_days: Option<i32>,
    pub last_activity_at: DateTime<Utc>,
    pub has_open_transaction: bool,
    pub is_root: bool,
    pub archived_at: Option<DateTime<Utc>>,
}

/// Effective retention after `INHERITED` is resolved up the parent
/// chain. The resolver returns the originating branch + the resolved
/// policy/TTL so the worker can audit the decision.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct EffectiveRetention {
    pub policy: RetentionPolicy,
    pub ttl_days: Option<i32>,
    /// The branch that supplied the policy (self for explicit, an
    /// ancestor for `INHERITED`, or `None` for an inheritance chain
    /// that only ever sees `INHERITED` — treated as `FOREVER`).
    pub source_branch_id: Option<Uuid>,
}

impl EffectiveRetention {
    /// Foundry default when the chain bottoms out without anyone
    /// setting an explicit policy: keep the branch around. The doc
    /// is explicit that retention should never silently delete data
    /// — `INHERITED` with no explicit ancestor falls back to
    /// `FOREVER`.
    pub fn default_forever() -> Self {
        Self {
            policy: RetentionPolicy::Forever,
            ttl_days: None,
            source_branch_id: None,
        }
    }
}

/// Resolve `INHERITED` against the row's parent chain. `index` MUST
/// contain every ancestor by id (caller responsibility).
pub fn resolve_effective_retention(
    row: &RetentionRow,
    index: &HashMap<Uuid, RetentionRow>,
) -> EffectiveRetention {
    let mut cursor = row;
    let mut visited = Vec::with_capacity(8);
    loop {
        if visited.contains(&cursor.id) {
            // Cycle guard — broken ancestry shouldn't pin a branch
            // to a non-existent policy. Default to FOREVER.
            return EffectiveRetention::default_forever();
        }
        visited.push(cursor.id);
        match cursor.policy {
            RetentionPolicy::Forever => {
                return EffectiveRetention {
                    policy: RetentionPolicy::Forever,
                    ttl_days: None,
                    source_branch_id: Some(cursor.id),
                };
            }
            RetentionPolicy::TtlDays => {
                return EffectiveRetention {
                    policy: RetentionPolicy::TtlDays,
                    ttl_days: cursor.ttl_days,
                    source_branch_id: Some(cursor.id),
                };
            }
            RetentionPolicy::Inherited => match cursor.parent_branch_id {
                Some(parent_id) => match index.get(&parent_id) {
                    Some(parent) => cursor = parent,
                    None => return EffectiveRetention::default_forever(),
                },
                None => return EffectiveRetention::default_forever(),
            },
        }
    }
}

/// Decide whether a branch is archive-eligible at `now`.
///
/// Foundry guarantees:
///   * roots are never archived (ancestry would be lost),
///   * a branch with an OPEN transaction is never archived (in-flight
///     work would silently disappear),
///   * already-archived branches are skipped.
pub fn is_archive_eligible(
    row: &RetentionRow,
    effective: &EffectiveRetention,
    now: DateTime<Utc>,
) -> bool {
    if row.archived_at.is_some() || row.is_root || row.has_open_transaction {
        return false;
    }
    match effective.policy {
        RetentionPolicy::Forever | RetentionPolicy::Inherited => false,
        RetentionPolicy::TtlDays => {
            let Some(days) = effective.ttl_days else {
                return false;
            };
            if days <= 0 {
                return false;
            }
            let cutoff = now - Duration::days(days as i64);
            row.last_activity_at < cutoff
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    fn ts(year: i32, month: u32, day: u32) -> DateTime<Utc> {
        Utc.with_ymd_and_hms(year, month, day, 0, 0, 0).unwrap()
    }

    fn row(id: u128, parent: Option<u128>, policy: RetentionPolicy, ttl: Option<i32>) -> RetentionRow {
        RetentionRow {
            id: Uuid::from_u128(id),
            parent_branch_id: parent.map(Uuid::from_u128),
            policy,
            ttl_days: ttl,
            last_activity_at: ts(2026, 1, 1),
            has_open_transaction: false,
            is_root: parent.is_none(),
            archived_at: None,
        }
    }

    fn index(rows: Vec<RetentionRow>) -> HashMap<Uuid, RetentionRow> {
        rows.into_iter().map(|r| (r.id, r)).collect()
    }

    #[test]
    fn explicit_forever_short_circuits_resolution() {
        let r = row(1, None, RetentionPolicy::Forever, None);
        let eff = resolve_effective_retention(&r, &index(vec![r.clone()]));
        assert_eq!(eff.policy, RetentionPolicy::Forever);
        assert_eq!(eff.source_branch_id, Some(r.id));
    }

    #[test]
    fn inherited_walks_to_first_explicit_ancestor() {
        let master = row(1, None, RetentionPolicy::Forever, None);
        let develop = row(2, Some(1), RetentionPolicy::Inherited, None);
        let feature = row(3, Some(2), RetentionPolicy::Inherited, None);
        let idx = index(vec![master.clone(), develop.clone(), feature.clone()]);

        let eff = resolve_effective_retention(&feature, &idx);
        assert_eq!(eff.policy, RetentionPolicy::Forever);
        assert_eq!(eff.source_branch_id, Some(master.id));
    }

    #[test]
    fn inherited_chain_without_explicit_defaults_to_forever() {
        let parent = row(1, None, RetentionPolicy::Inherited, None);
        let child = row(2, Some(1), RetentionPolicy::Inherited, None);
        let idx = index(vec![parent, child.clone()]);
        let eff = resolve_effective_retention(&child, &idx);
        assert_eq!(eff.policy, RetentionPolicy::Forever);
        assert_eq!(eff.source_branch_id, None);
    }

    #[test]
    fn ttl_eligibility_respects_open_transaction_invariant() {
        let now = ts(2026, 6, 1);
        let mut feature = row(1, None, RetentionPolicy::TtlDays, Some(30));
        feature.is_root = false;
        feature.parent_branch_id = Some(Uuid::from_u128(99));
        feature.last_activity_at = ts(2026, 1, 1);
        feature.has_open_transaction = true;
        let eff = EffectiveRetention {
            policy: RetentionPolicy::TtlDays,
            ttl_days: Some(30),
            source_branch_id: Some(feature.id),
        };
        assert!(!is_archive_eligible(&feature, &eff, now));
    }

    #[test]
    fn ttl_eligibility_archives_stale_non_root_branch() {
        let now = ts(2026, 6, 1);
        let mut feature = row(1, None, RetentionPolicy::TtlDays, Some(30));
        feature.is_root = false;
        feature.parent_branch_id = Some(Uuid::from_u128(99));
        feature.last_activity_at = ts(2026, 4, 1); // 60 days ago
        let eff = EffectiveRetention {
            policy: RetentionPolicy::TtlDays,
            ttl_days: Some(30),
            source_branch_id: Some(feature.id),
        };
        assert!(is_archive_eligible(&feature, &eff, now));
    }

    #[test]
    fn forever_branches_are_never_eligible() {
        let now = ts(2026, 6, 1);
        let mut master = row(1, None, RetentionPolicy::Forever, None);
        master.last_activity_at = ts(2025, 1, 1);
        let eff = EffectiveRetention {
            policy: RetentionPolicy::Forever,
            ttl_days: None,
            source_branch_id: Some(master.id),
        };
        assert!(!is_archive_eligible(&master, &eff, now));
    }

    #[test]
    fn root_branches_are_never_eligible_even_with_ttl() {
        let now = ts(2026, 6, 1);
        let mut root = row(1, None, RetentionPolicy::TtlDays, Some(1));
        root.is_root = true;
        root.last_activity_at = ts(2026, 4, 1);
        let eff = EffectiveRetention {
            policy: RetentionPolicy::TtlDays,
            ttl_days: Some(1),
            source_branch_id: Some(root.id),
        };
        assert!(!is_archive_eligible(&root, &eff, now));
    }
}
