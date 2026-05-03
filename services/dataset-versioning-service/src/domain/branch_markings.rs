//! Branch markings inheritance — Foundry "Branch security" doc.
//!
//! ## The rule
//!
//! When a child branch is created, every marking the *parent* carries
//! at that exact moment is copied into `branch_markings_snapshot`
//! with `source = PARENT`. The set is **frozen at creation time**.
//!
//! Subsequent markings the user attaches directly to the child carry
//! `source = EXPLICIT` and stack on top of the inherited floor. The
//! effective marking set is `PARENT ∪ EXPLICIT`.
//!
//! Markings added to the *parent* AFTER the child's creation do **not**
//! propagate. This is deliberate per the doc § "Best practices and
//! technical details": late-propagation would let a parent owner
//! retroactively raise a child's clearance floor without the child
//! owner's consent. The snapshot-at-creation semantics make every
//! security-relevant change explicit.
//!
//! ## Wire shape
//!
//! ```json
//! {
//!   "effective": ["pii", "hipaa"],
//!   "explicit":  ["hipaa"],
//!   "inherited_from_parent": ["pii"]
//! }
//! ```
//!
//! UI uses `inherited_from_parent` to render the "inherited from
//! parent" badge variant in `MarkingBadge.svelte`.

use std::collections::BTreeSet;

use serde::{Deserialize, Serialize};
use uuid::Uuid;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum MarkingSource {
    Parent,
    Explicit,
}

impl MarkingSource {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Parent => "PARENT",
            Self::Explicit => "EXPLICIT",
        }
    }
}

/// One row from `branch_markings_snapshot`.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BranchMarking {
    pub branch_id: Uuid,
    pub marking_id: Uuid,
    pub source: MarkingSource,
}

/// Wire-shape returned by `GET /branches/{branch}/markings`.
#[derive(Debug, Clone, Serialize)]
pub struct BranchMarkingsView {
    pub effective: Vec<Uuid>,
    pub explicit: Vec<Uuid>,
    pub inherited_from_parent: Vec<Uuid>,
}

impl BranchMarkingsView {
    /// Project a snapshot row set into the API response shape.
    pub fn from_rows(rows: &[BranchMarking]) -> Self {
        let mut explicit: BTreeSet<Uuid> = BTreeSet::new();
        let mut inherited: BTreeSet<Uuid> = BTreeSet::new();
        for r in rows {
            match r.source {
                MarkingSource::Parent => {
                    inherited.insert(r.marking_id);
                }
                MarkingSource::Explicit => {
                    explicit.insert(r.marking_id);
                }
            }
        }
        let effective: BTreeSet<Uuid> = explicit.union(&inherited).copied().collect();
        Self {
            effective: effective.into_iter().collect(),
            explicit: explicit.into_iter().collect(),
            inherited_from_parent: inherited.into_iter().collect(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn marking(branch: u128, marking: u128, source: MarkingSource) -> BranchMarking {
        BranchMarking {
            branch_id: Uuid::from_u128(branch),
            marking_id: Uuid::from_u128(marking),
            source,
        }
    }

    #[test]
    fn explicit_and_inherited_partition_into_response_shape() {
        let m_pii = Uuid::from_u128(1);
        let m_hipaa = Uuid::from_u128(2);
        let view = BranchMarkingsView::from_rows(&[
            marking(10, 1, MarkingSource::Parent),
            marking(10, 2, MarkingSource::Explicit),
        ]);
        assert!(view.effective.contains(&m_pii));
        assert!(view.effective.contains(&m_hipaa));
        assert_eq!(view.explicit, vec![m_hipaa]);
        assert_eq!(view.inherited_from_parent, vec![m_pii]);
    }

    #[test]
    fn marking_present_under_both_sources_dedupes_into_effective_once() {
        // A marking can show up once — the snapshot table primary key
        // is `(branch_id, marking_id)`, so a single id can have only
        // one source. Validate the view stays consistent if a future
        // refactor relaxes that.
        let m = Uuid::from_u128(7);
        let view = BranchMarkingsView::from_rows(&[
            marking(10, 7, MarkingSource::Parent),
            marking(10, 7, MarkingSource::Explicit),
        ]);
        assert_eq!(view.effective, vec![m]);
    }
}
