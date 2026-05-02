//! Canonical marking primitives.
//!
//! A *marking* is a classification label (e.g. `public`, `confidential`,
//! `pii`, `restricted`) attached to a dataset (and, transitively, to
//! everything derived from it). The Security & Governance pillar of the
//! Datasets / Lineage docs requires markings to **inherit upstream**:
//! a dataset that derives from `RESTRICTED` becomes `RESTRICTED` itself,
//! even if no human ever attached the label to it directly.
//!
//! These primitives are the lingua franca for that propagation:
//!
//! * [`MarkingId`] is the stable identifier of a marking definition,
//!   stored in the `markings` master table managed by
//!   `ontology-security-service`. We model it as a typed UUID so it
//!   can't accidentally be confused with a dataset RID or a user ID.
//! * [`MarkingSource`] explains *why* a marking applies to a dataset —
//!   either a user attached it (`Direct`) or it propagated from an
//!   upstream dataset (`InheritedFromUpstream`). Knowing the source is
//!   critical for the UI ("inherited from <rid>" badges) and for audit:
//!   removing the upstream removes the inheritance, but a direct
//!   marking sticks.
//! * [`EffectiveMarking`] is the per-dataset projection: an `id` plus
//!   the `source` that put it there. The ordered set of these is what
//!   `compute_effective_markings(rid)` (T3.2) returns and what the
//!   enforcement middleware (T3.3) checks against the caller's
//!   clearances.
//!
//! Storage layout (matches the `dataset_markings` migration):
//!
//! ```text
//!   dataset_rid │ marking_id │ source        │ inherited_from
//!   ─────────────┼────────────┼───────────────┼────────────────
//!   ds.A         │ pii        │ direct        │ NULL
//!   ds.B         │ pii        │ inherited     │ ds.A
//! ```

use std::str::FromStr;

use serde::{Deserialize, Serialize};
use uuid::Uuid;

/// Stable identifier of a marking definition.
///
/// Markings are catalogued centrally; new markings can't be invented
/// inline by a service. An `MarkingId` therefore always corresponds to
/// a row in the `markings` table.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(transparent)]
pub struct MarkingId(pub Uuid);

impl MarkingId {
    /// Construct from an existing UUID (e.g. read from the DB).
    pub const fn from_uuid(id: Uuid) -> Self {
        Self(id)
    }

    /// Mint a brand-new id for a freshly created marking.
    pub fn new() -> Self {
        Self(Uuid::now_v7())
    }

    /// Underlying UUID value.
    pub const fn as_uuid(&self) -> Uuid {
        self.0
    }
}

impl Default for MarkingId {
    fn default() -> Self {
        Self::new()
    }
}

impl std::fmt::Display for MarkingId {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        self.0.fmt(f)
    }
}

/// Failure parsing a `MarkingId` from a string.
#[derive(Debug, thiserror::Error)]
#[error("invalid MarkingId: {0}")]
pub struct InvalidMarkingId(String);

impl FromStr for MarkingId {
    type Err = InvalidMarkingId;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        Uuid::parse_str(s)
            .map(Self)
            .map_err(|_| InvalidMarkingId(s.to_owned()))
    }
}

/// Why a marking applies to a dataset.
///
/// Serialised as a tagged enum to keep the wire format unambiguous and
/// so `inherited_from` is omitted for direct markings:
///
/// ```json
/// { "kind": "direct" }
/// { "kind": "inherited_from_upstream", "upstream_rid": "ri.foundry.main.dataset.…" }
/// ```
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum MarkingSource {
    /// A user explicitly attached this marking to the dataset.
    Direct,
    /// The marking propagated from an upstream dataset along the
    /// lineage graph. `upstream_rid` is the immediate ancestor that
    /// caused the inheritance — the chain may be longer, but storing
    /// the closest hop is enough to answer "where does this come from?"
    /// in the UI without a graph walk.
    InheritedFromUpstream { upstream_rid: String },
}

impl MarkingSource {
    /// Convenience constructor used by call sites that already have a
    /// `&str` upstream RID.
    pub fn inherited_from(upstream_rid: impl Into<String>) -> Self {
        Self::InheritedFromUpstream {
            upstream_rid: upstream_rid.into(),
        }
    }

    /// True when the source is [`MarkingSource::Direct`]. Useful for
    /// badge styling in the UI.
    pub const fn is_direct(&self) -> bool {
        matches!(self, Self::Direct)
    }

    /// Returns the upstream RID for inherited markings, `None` for
    /// direct ones.
    pub fn upstream_rid(&self) -> Option<&str> {
        match self {
            Self::InheritedFromUpstream { upstream_rid } => Some(upstream_rid.as_str()),
            Self::Direct => None,
        }
    }
}

/// Per-dataset projection: a marking and the reason it applies.
///
/// Two `EffectiveMarking`s with the same `id` but different `source`
/// are considered distinct (a marking can be both directly attached
/// *and* inherited from multiple upstreams; the union of all of those
/// is what the enforcement layer cares about).
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct EffectiveMarking {
    pub id: MarkingId,
    pub source: MarkingSource,
}

impl EffectiveMarking {
    pub fn direct(id: MarkingId) -> Self {
        Self {
            id,
            source: MarkingSource::Direct,
        }
    }

    pub fn inherited(id: MarkingId, upstream_rid: impl Into<String>) -> Self {
        Self {
            id,
            source: MarkingSource::inherited_from(upstream_rid),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn marking_source_serialises_with_kind_tag() {
        let direct = MarkingSource::Direct;
        let inherited = MarkingSource::inherited_from("ri.foundry.main.dataset.abc");

        let direct_json = serde_json::to_value(&direct).unwrap();
        assert_eq!(direct_json, serde_json::json!({ "kind": "direct" }));

        let inherited_json = serde_json::to_value(&inherited).unwrap();
        assert_eq!(
            inherited_json,
            serde_json::json!({
                "kind": "inherited_from_upstream",
                "upstream_rid": "ri.foundry.main.dataset.abc",
            })
        );
    }

    #[test]
    fn marking_source_helpers() {
        let direct = MarkingSource::Direct;
        assert!(direct.is_direct());
        assert_eq!(direct.upstream_rid(), None);

        let inherited = MarkingSource::inherited_from("ri.x");
        assert!(!inherited.is_direct());
        assert_eq!(inherited.upstream_rid(), Some("ri.x"));
    }

    #[test]
    fn marking_id_roundtrips_through_string() {
        let id = MarkingId::new();
        let parsed: MarkingId = id.to_string().parse().unwrap();
        assert_eq!(id, parsed);
    }

    #[test]
    fn invalid_marking_id_string_is_rejected() {
        let err = "not-a-uuid".parse::<MarkingId>().unwrap_err();
        assert!(err.to_string().contains("not-a-uuid"));
    }

    #[test]
    fn effective_marking_constructors() {
        let id = MarkingId::new();
        let direct = EffectiveMarking::direct(id);
        assert!(direct.source.is_direct());
        let inherited = EffectiveMarking::inherited(id, "ri.foundry.main.dataset.parent");
        assert_eq!(
            inherited.source.upstream_rid(),
            Some("ri.foundry.main.dataset.parent")
        );
    }
}
