//! Foundry-style **media reference**: the typed pointer that travels in
//! dataset cells / ontology object properties to address a single media
//! item inside a media set.
//!
//! Foundry stores these as JSON blobs with camelCase keys. A property of
//! type `mediaReference` on an Object Type therefore round-trips through
//! the shape:
//!
//! ```json
//! {
//!   "mediaSetRid": "ri.foundry.main.media_set.<uuid>",
//!   "mediaItemRid": "ri.foundry.main.media_item.<uuid>",
//!   "branch": "master",
//!   "schema": "IMAGE"
//! }
//! ```
//!
//! Keep this in sync with the proto `MediaSetSchema` in
//! `proto/media_set/media_set.proto` — both must enumerate the same
//! variants because the schema string travels on the wire.

use std::str::FromStr;

use serde::{Deserialize, Serialize};

/// Schema (high-level media kind) of a media set / item.
///
/// Serialised as the upper-snake-case string that Foundry exposes in
/// media-reference JSON (`"IMAGE"`, `"AUDIO"`, …) — not as a numeric
/// discriminant — so the wire format stays human-readable and matches
/// the proto enum value names without the `MEDIA_SET_SCHEMA_` prefix.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum MediaSetSchema {
    Image,
    Audio,
    Video,
    Document,
    Spreadsheet,
    Email,
}

impl MediaSetSchema {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Image => "IMAGE",
            Self::Audio => "AUDIO",
            Self::Video => "VIDEO",
            Self::Document => "DOCUMENT",
            Self::Spreadsheet => "SPREADSHEET",
            Self::Email => "EMAIL",
        }
    }
}

impl std::fmt::Display for MediaSetSchema {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

/// Failure parsing a [`MediaSetSchema`] from its string form.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
#[error("unknown media set schema `{0}` (expected IMAGE|AUDIO|VIDEO|DOCUMENT|SPREADSHEET|EMAIL)")]
pub struct UnknownMediaSetSchema(pub String);

impl FromStr for MediaSetSchema {
    type Err = UnknownMediaSetSchema;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_ascii_uppercase().as_str() {
            "IMAGE" => Ok(Self::Image),
            "AUDIO" => Ok(Self::Audio),
            "VIDEO" => Ok(Self::Video),
            "DOCUMENT" => Ok(Self::Document),
            "SPREADSHEET" => Ok(Self::Spreadsheet),
            "EMAIL" => Ok(Self::Email),
            other => Err(UnknownMediaSetSchema(other.to_string())),
        }
    }
}

/// Pointer to a single media item inside a media set, suitable for
/// embedding in a dataset row or an ontology object property.
///
/// Keep field order stable: serde-json preserves struct field order in
/// the emitted JSON, and downstream consumers (dataset preview, OSDK,
/// Workshop) match on the camelCase key set rather than position.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MediaReference {
    pub media_set_rid: String,
    pub media_item_rid: String,
    pub branch: String,
    pub schema: MediaSetSchema,
}

impl MediaReference {
    pub fn new(
        media_set_rid: impl Into<String>,
        media_item_rid: impl Into<String>,
        branch: impl Into<String>,
        schema: MediaSetSchema,
    ) -> Self {
        Self {
            media_set_rid: media_set_rid.into(),
            media_item_rid: media_item_rid.into(),
            branch: branch.into(),
            schema,
        }
    }

    /// Encode as the Foundry-compatible JSON string stored inside dataset
    /// cells / ontology property values.
    pub fn to_foundry_json(&self) -> Result<String, serde_json::Error> {
        serde_json::to_string(self)
    }

    /// Parse from the Foundry-compatible JSON string.
    pub fn from_foundry_json(s: &str) -> Result<Self, serde_json::Error> {
        serde_json::from_str(s)
    }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn schema_serialises_as_screaming_snake() {
        let s = serde_json::to_string(&MediaSetSchema::Image).unwrap();
        assert_eq!(s, "\"IMAGE\"");
        let parsed: MediaSetSchema = serde_json::from_str("\"SPREADSHEET\"").unwrap();
        assert_eq!(parsed, MediaSetSchema::Spreadsheet);
    }

    #[test]
    fn schema_from_str_is_case_insensitive() {
        assert_eq!(
            "image".parse::<MediaSetSchema>().unwrap(),
            MediaSetSchema::Image
        );
        assert!("nope".parse::<MediaSetSchema>().is_err());
    }

    #[test]
    fn media_reference_roundtrips_through_foundry_json() {
        let original = MediaReference::new(
            "ri.foundry.main.media_set.018f2f1c-aaaa-bbbb-cccc-000000000001",
            "ri.foundry.main.media_item.018f2f1c-aaaa-bbbb-cccc-000000000002",
            "master",
            MediaSetSchema::Image,
        );
        let json = original.to_foundry_json().unwrap();
        let parsed = MediaReference::from_foundry_json(&json).unwrap();
        assert_eq!(parsed, original);
    }

    #[test]
    fn media_reference_uses_camel_case_keys() {
        let mr = MediaReference::new("ms", "mi", "master", MediaSetSchema::Audio);
        let value: serde_json::Value = serde_json::from_str(&mr.to_foundry_json().unwrap()).unwrap();
        assert_eq!(
            value,
            serde_json::json!({
                "mediaSetRid": "ms",
                "mediaItemRid": "mi",
                "branch": "master",
                "schema": "AUDIO",
            })
        );
    }
}
