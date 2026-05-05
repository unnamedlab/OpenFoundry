//! Foundry-style validation for the `media_reference` property type
//! that goes beyond the structural shape check in
//! [`crate::domain::type_system::validate_property_value`].
//!
//! `validate_property_value` is a pure-Rust shape gate. It accepts a
//! string OR an object with a non-empty `uri`/`url` — fine for the
//! pre-H6 stub, but H6 lifts media-reference properties into the
//! Ontology proper and demands two extra invariants from
//! `Using media in the Ontology.md` + `Upload media.md`:
//!
//!   1. **Set existence.** The `mediaSetRid` must address a media set
//!      that exists; an unresolved RID writes a dangling pointer into
//!      object state.
//!   2. **Clearance covers markings.** The user editing the property
//!      must hold every marking on the backing media set
//!      (Foundry: "Adding a media set to your ontology delegates
//!      access control from the media set to the ontology"). The
//!      kernel cannot itself talk to Cedar / Postgres, so we accept
//!      the resolved markings via callback and run the inclusion
//!      check ourselves.
//!
//! Callers (typically `ontology-actions-service` on action submit
//! and the inline-edit path in `handlers/properties.rs`) build a
//! [`MediaReferenceContext`], wire its closures to the platform
//! lookups, and pass it down. The kernel stays dep-light.

use std::collections::BTreeSet;

use serde_json::Value;

/// Canonical Foundry payload for a media-reference property value.
/// Mirrors the shape `core_models::MediaReference` emits via
/// [`MediaReference::to_foundry_json`]; we keep this struct local to
/// the kernel so the kernel does not have to depend on `core-models`.
///
/// The `branch` and `schema` fields are optional in pre-H6 callers
/// (legacy code that wrote string-form pointers); validators MUST
/// surface a clear error if either is missing on H6 surfaces.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ParsedMediaReference {
    pub media_set_rid: String,
    pub media_item_rid: String,
    pub branch: Option<String>,
    pub schema: Option<String>,
}

/// Lookup result for a media set the validator is consulting. The
/// caller resolves this from `media-sets-service` (Postgres) or a
/// cached read; the validator only needs to know whether the set
/// exists and which markings it carries.
#[derive(Debug, Clone)]
pub struct ResolvedMediaSet {
    pub media_set_rid: String,
    /// Lower-cased markings the set is tagged with. Empty = no
    /// markings (the validator allows the edit unconditionally).
    pub markings: Vec<String>,
}

/// Caller-supplied context used by the validator. Two closures —
/// one for set lookup, one for clearance — kept tiny so the action
/// handler can wire them against whichever store it owns without the
/// kernel taking on a new dep.
pub struct MediaReferenceContext<'a> {
    pub resolve_set: Box<dyn Fn(&str) -> Option<ResolvedMediaSet> + Send + Sync + 'a>,
    pub user_clearances: Vec<String>,
}

impl std::fmt::Debug for MediaReferenceContext<'_> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("MediaReferenceContext")
            .field("user_clearances", &self.user_clearances)
            .finish_non_exhaustive()
    }
}

/// Failure modes — distinct variants so the action handler can map
/// each to the right HTTP status without parsing strings.
#[derive(Debug, Clone, thiserror::Error, PartialEq, Eq)]
pub enum MediaReferenceValidationError {
    #[error("media_reference value must be a JSON object on H6 ontology surfaces")]
    NotAnObject,
    #[error("media_reference is missing field `{0}`")]
    MissingField(&'static str),
    #[error("media_reference field `{0}` must be a non-empty string")]
    EmptyField(&'static str),
    #[error("media set `{0}` does not exist")]
    UnknownMediaSet(String),
    #[error("missing clearance: {missing}")]
    InsufficientClearance { missing: String },
}

/// Parse the camelCase Foundry payload (`mediaSetRid`,
/// `mediaItemRid`, …) AND the snake_case Rust payload that
/// `core_models::MediaReference::to_foundry_json` emits. We accept
/// both because the OSDK round-trips through camelCase but the Rust
/// services serialise via the struct.
fn parse(value: &Value) -> Result<ParsedMediaReference, MediaReferenceValidationError> {
    let object = value
        .as_object()
        .ok_or(MediaReferenceValidationError::NotAnObject)?;

    let pull = |camel: &'static str, snake: &'static str| -> Option<&str> {
        object
            .get(camel)
            .or_else(|| object.get(snake))
            .and_then(Value::as_str)
            .map(str::trim)
            .filter(|v| !v.is_empty())
    };

    let media_set_rid = pull("mediaSetRid", "media_set_rid")
        .ok_or(MediaReferenceValidationError::MissingField("mediaSetRid"))?
        .to_string();
    let media_item_rid = pull("mediaItemRid", "media_item_rid")
        .ok_or(MediaReferenceValidationError::MissingField("mediaItemRid"))?
        .to_string();
    let branch = pull("branch", "branch").map(|s| s.to_string());
    let schema = pull("schema", "schema").map(|s| s.to_string());

    Ok(ParsedMediaReference {
        media_set_rid,
        media_item_rid,
        branch,
        schema,
    })
}

/// Run the H6 contract: shape parse → set exists → clearances cover
/// the set's markings. Returns the parsed reference on success so
/// the caller can persist the canonical form (avoids a re-parse).
pub fn validate(
    value: &Value,
    ctx: &MediaReferenceContext<'_>,
) -> Result<ParsedMediaReference, MediaReferenceValidationError> {
    let parsed = parse(value)?;
    let resolved = (ctx.resolve_set)(&parsed.media_set_rid).ok_or_else(|| {
        MediaReferenceValidationError::UnknownMediaSet(parsed.media_set_rid.clone())
    })?;
    if !covers_clearance(&ctx.user_clearances, &resolved.markings) {
        let missing = first_missing_marking(&ctx.user_clearances, &resolved.markings)
            .unwrap_or_else(|| resolved.markings.join(", "));
        return Err(MediaReferenceValidationError::InsufficientClearance { missing });
    }
    Ok(parsed)
}

fn covers_clearance(user_clearances: &[String], required_markings: &[String]) -> bool {
    let user: BTreeSet<&str> = user_clearances
        .iter()
        .map(|s| s.trim())
        .filter(|s| !s.is_empty())
        .collect();
    required_markings
        .iter()
        .map(|s| s.trim())
        .filter(|s| !s.is_empty())
        .all(|marking| user.contains(marking))
}

fn first_missing_marking(user: &[String], required: &[String]) -> Option<String> {
    let user: BTreeSet<&str> = user
        .iter()
        .map(|s| s.trim())
        .filter(|s| !s.is_empty())
        .collect();
    required
        .iter()
        .map(|s| s.trim())
        .find(|marking| !marking.is_empty() && !user.contains(marking))
        .map(|s| s.to_string())
}

/// Build a [`MediaReferenceContext`] from an in-memory map — useful
/// in tests + the inline-edit fast path that already loaded the set.
pub fn context_from_map<'a>(
    sets: std::collections::HashMap<String, ResolvedMediaSet>,
    user_clearances: Vec<String>,
) -> MediaReferenceContext<'a> {
    MediaReferenceContext {
        resolve_set: Box::new(move |rid| sets.get(rid).cloned()),
        user_clearances,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::collections::HashMap;

    fn ctx(set_rid: &str, markings: Vec<&str>, clearances: Vec<&str>) -> MediaReferenceContext<'static> {
        let mut map = HashMap::new();
        map.insert(
            set_rid.to_string(),
            ResolvedMediaSet {
                media_set_rid: set_rid.to_string(),
                markings: markings.into_iter().map(String::from).collect(),
            },
        );
        context_from_map(map, clearances.into_iter().map(String::from).collect())
    }

    #[test]
    fn accepts_camel_case_payload() {
        let v = json!({
            "mediaSetRid": "ri.foundry.main.media_set.abc",
            "mediaItemRid": "ri.foundry.main.media_item.def",
            "branch": "main",
            "schema": "IMAGE"
        });
        let ctx = ctx("ri.foundry.main.media_set.abc", vec!["public"], vec!["public"]);
        let parsed = validate(&v, &ctx).expect("happy path");
        assert_eq!(parsed.branch.as_deref(), Some("main"));
        assert_eq!(parsed.schema.as_deref(), Some("IMAGE"));
    }

    #[test]
    fn rejects_unknown_media_set() {
        let v = json!({
            "mediaSetRid": "ri.foundry.main.media_set.absent",
            "mediaItemRid": "x"
        });
        let ctx = ctx("ri.foundry.main.media_set.other", vec![], vec!["public"]);
        let err = validate(&v, &ctx).unwrap_err();
        assert!(matches!(
            err,
            MediaReferenceValidationError::UnknownMediaSet(_)
        ));
    }

    #[test]
    fn rejects_when_clearances_miss_a_marking() {
        let v = json!({
            "mediaSetRid": "ri.foundry.main.media_set.classified",
            "mediaItemRid": "x"
        });
        let ctx = ctx(
            "ri.foundry.main.media_set.classified",
            vec!["public", "secret"],
            vec!["public"],
        );
        let err = validate(&v, &ctx).unwrap_err();
        match err {
            MediaReferenceValidationError::InsufficientClearance { missing } => {
                assert_eq!(missing, "secret")
            }
            other => panic!("expected InsufficientClearance, got {other:?}"),
        }
    }

    #[test]
    fn rejects_missing_required_field() {
        let v = json!({"mediaSetRid": "rid"});
        let ctx = ctx("rid", vec![], vec![]);
        let err = validate(&v, &ctx).unwrap_err();
        assert_eq!(
            err,
            MediaReferenceValidationError::MissingField("mediaItemRid")
        );
    }

    #[test]
    fn rejects_non_object_payload() {
        let v = json!("just-a-string");
        let ctx = ctx("any", vec![], vec![]);
        assert_eq!(
            validate(&v, &ctx).unwrap_err(),
            MediaReferenceValidationError::NotAnObject
        );
    }

    #[test]
    fn accepts_snake_case_payload_too() {
        let v = json!({
            "media_set_rid": "ri.foundry.main.media_set.abc",
            "media_item_rid": "ri.foundry.main.media_item.def"
        });
        let ctx = ctx("ri.foundry.main.media_set.abc", vec![], vec![]);
        validate(&v, &ctx).unwrap();
    }
}
