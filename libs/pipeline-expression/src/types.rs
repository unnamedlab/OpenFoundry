//! Pipeline Builder runtime types.
//!
//! Mirrors the canonical lattice documented in Foundry's "Supported
//! languages" reference. Numeric promotion follows the SQL-flavoured
//! order Boolean < Integer < Long < Double < Decimal; cross-family
//! promotion (e.g. String → Integer) requires an explicit `cast`.

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "UPPERCASE")]
pub enum PipelineType {
    Boolean,
    Integer,
    Long,
    Double,
    Decimal,
    String,
    Date,
    Timestamp,
    Geometry,
    Array {
        inner: Box<PipelineType>,
    },
    Struct {
        fields: Vec<(String, PipelineType)>,
    },
}

impl PipelineType {
    pub fn is_numeric(&self) -> bool {
        matches!(
            self,
            PipelineType::Integer
                | PipelineType::Long
                | PipelineType::Double
                | PipelineType::Decimal
        )
    }

    pub fn is_textual(&self) -> bool {
        matches!(self, PipelineType::String)
    }

    pub fn is_temporal(&self) -> bool {
        matches!(self, PipelineType::Date | PipelineType::Timestamp)
    }

    pub fn array_of(inner: PipelineType) -> Self {
        PipelineType::Array { inner: Box::new(inner) }
    }

    pub fn struct_of(fields: Vec<(impl Into<String>, PipelineType)>) -> Self {
        PipelineType::Struct {
            fields: fields.into_iter().map(|(n, t)| (n.into(), t)).collect(),
        }
    }
}

/// Numeric promotion order. Booleans cannot silently widen to numerics —
/// only `cast` does that. Cross-family promotion (numeric/string/date)
/// is rejected.
fn numeric_rank(ty: &PipelineType) -> Option<u8> {
    match ty {
        PipelineType::Integer => Some(0),
        PipelineType::Long => Some(1),
        PipelineType::Double => Some(2),
        PipelineType::Decimal => Some(3),
        _ => None,
    }
}

/// Returns `true` when `from` can be implicitly promoted to `to`. A
/// type is always promotable to itself.
pub fn can_promote(from: &PipelineType, to: &PipelineType) -> bool {
    if from == to {
        return true;
    }
    if let (Some(a), Some(b)) = (numeric_rank(from), numeric_rank(to)) {
        return a <= b;
    }
    // Date silently widens to Timestamp (a Timestamp covers a Date with
    // the time component zeroed). The reverse requires an explicit cast.
    if matches!(from, PipelineType::Date) && matches!(to, PipelineType::Timestamp) {
        return true;
    }
    // Array<T> promotes elementwise.
    if let (
        PipelineType::Array { inner: a },
        PipelineType::Array { inner: b },
    ) = (from, to)
    {
        return can_promote(a, b);
    }
    false
}

/// Compute the least upper bound of two types. Returns `None` when no
/// common supertype exists (i.e. the operation is a type error).
pub fn promote(left: &PipelineType, right: &PipelineType) -> Option<PipelineType> {
    if left == right {
        return Some(left.clone());
    }
    if let (Some(a), Some(b)) = (numeric_rank(left), numeric_rank(right)) {
        let pick = if a >= b { left } else { right };
        return Some(pick.clone());
    }
    if (matches!(left, PipelineType::Date) && matches!(right, PipelineType::Timestamp))
        || (matches!(left, PipelineType::Timestamp) && matches!(right, PipelineType::Date))
    {
        return Some(PipelineType::Timestamp);
    }
    if let (
        PipelineType::Array { inner: a },
        PipelineType::Array { inner: b },
    ) = (left, right)
    {
        return promote(a, b).map(PipelineType::array_of);
    }
    None
}
