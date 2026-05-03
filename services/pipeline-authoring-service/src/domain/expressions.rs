//! Pipeline Builder expressions for media references.
//!
//! Foundry sources:
//! * `Is valid media reference.md`
//!   — boolean expression that returns `true` when the input string is
//!     a valid Foundry media reference JSON.
//! * `Construct delegated media Gotham identifier (GID).md`
//!   — string expression that emits a `delegatedMediaGid` from a media
//!     set RID + media item RID.
//!
//! The expressions live as plain async-free functions so the runtime
//! engine can call them inline without an HTTP hop.
//!
//! Wire shape
//! ----------
//! Both expressions are addressable through the same dispatch the
//! transform nodes use — the engine receives an `Expression` JSON
//! object of the form
//!
//! ```json
//! { "kind": "is_valid_media_reference", "input": "<string>" }
//! { "kind": "construct_delegated_media_gid",
//!   "media_set_rid": "ri.foundry.main.media_set.…",
//!   "media_item_rid": "ri.foundry.main.media_item.…",
//!   "branch": "main",
//!   "schema": "IMAGE" }
//! ```
//!
//! and dispatches to [`evaluate`] below. Validation runs on the same
//! payload before evaluation so the compiler can surface configuration
//! errors before the run starts.

use core_models::{MediaReference, MediaSetSchema};
use serde::{Deserialize, Serialize};
use serde_json::Value;

/// Discriminator for the two media-reference expressions.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum MediaExpressionKind {
    IsValidMediaReference,
    ConstructDelegatedMediaGid,
}

/// `IsValidMediaReference(input)` — returns `true` iff `input` parses
/// as a Foundry media-reference JSON
/// (`{ "mediaSetRid": "...", "mediaItemRid": "...", "branch": "...", "schema": "..." }`).
///
/// Wire payload: `{ "input": "<string>" }`. The string is normally a
/// dataset cell value, but the validator accepts any JSON-string-shaped
/// input — non-strings are surfaced as a validation error.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct IsValidMediaReferenceArgs {
    pub input: String,
}

/// `ConstructDelegatedMediaGid` — composes the canonical Gotham
/// identifier string used to refer to a media item from outside Foundry.
///
/// Format: `gid:foundry/<media_set_rid>/<branch>/<media_item_rid>`.
/// (Schema is included as the Foundry `MediaReference` JSON; the GID
/// is the URL-shaped lookup key the OSDK uses.)
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ConstructDelegatedMediaGidArgs {
    pub media_set_rid: String,
    pub media_item_rid: String,
    #[serde(default = "default_branch")]
    pub branch: String,
    pub schema: MediaSetSchema,
}

fn default_branch() -> String {
    "main".to_string()
}

/// Validate the per-kind args payload. Empty Vec = valid; otherwise a
/// list of human-readable errors. Validation is independent of
/// evaluation so the pipeline compiler can surface errors at authoring
/// time without executing anything.
pub fn validate(kind: MediaExpressionKind, args: &Value) -> Vec<String> {
    let mut errors = Vec::new();
    match kind {
        MediaExpressionKind::IsValidMediaReference => {
            if let Err(err) = serde_json::from_value::<IsValidMediaReferenceArgs>(args.clone()) {
                errors.push(format!("is_valid_media_reference: {err}"));
            }
        }
        MediaExpressionKind::ConstructDelegatedMediaGid => {
            match serde_json::from_value::<ConstructDelegatedMediaGidArgs>(args.clone()) {
                Ok(a) => {
                    if !a.media_set_rid.starts_with("ri.foundry.main.media_set.") {
                        errors.push(format!(
                            "construct_delegated_media_gid.media_set_rid `{}` is not a Foundry \
                             media-set RID",
                            a.media_set_rid
                        ));
                    }
                    if !a.media_item_rid.starts_with("ri.foundry.main.media_item.") {
                        errors.push(format!(
                            "construct_delegated_media_gid.media_item_rid `{}` is not a Foundry \
                             media-item RID",
                            a.media_item_rid
                        ));
                    }
                    if a.branch.trim().is_empty() {
                        errors.push("construct_delegated_media_gid.branch must not be empty".into());
                    }
                }
                Err(err) => errors.push(format!("construct_delegated_media_gid: {err}")),
            }
        }
    }
    errors
}

/// Evaluate the expression. Returns the JSON-encoded result the engine
/// hands back to the calling node:
/// * `IsValidMediaReference` → `Value::Bool`.
/// * `ConstructDelegatedMediaGid` → `Value::String`.
///
/// `Err` is returned on validation failures (so callers can short-circuit
/// without running anything).
pub fn evaluate(kind: MediaExpressionKind, args: &Value) -> Result<Value, Vec<String>> {
    let errors = validate(kind, args);
    if !errors.is_empty() {
        return Err(errors);
    }
    match kind {
        MediaExpressionKind::IsValidMediaReference => {
            let payload: IsValidMediaReferenceArgs = serde_json::from_value(args.clone()).unwrap();
            Ok(Value::Bool(is_valid_media_reference(&payload.input)))
        }
        MediaExpressionKind::ConstructDelegatedMediaGid => {
            let payload: ConstructDelegatedMediaGidArgs =
                serde_json::from_value(args.clone()).unwrap();
            Ok(Value::String(construct_delegated_media_gid(
                &payload.media_set_rid,
                &payload.media_item_rid,
                &payload.branch,
                payload.schema,
            )))
        }
    }
}

/// Returns true iff `input` parses as a Foundry media reference
/// (camelCase keys, all four fields present, schema is one of the six
/// supported values).
pub fn is_valid_media_reference(input: &str) -> bool {
    MediaReference::from_foundry_json(input).is_ok()
}

/// Compose a delegated media Gotham identifier from a media-set RID, a
/// media-item RID, a branch and a schema. The shape mirrors the one
/// the OSDK uses to address a media item from outside Foundry.
pub fn construct_delegated_media_gid(
    media_set_rid: &str,
    media_item_rid: &str,
    branch: &str,
    schema: MediaSetSchema,
) -> String {
    format!(
        "gid:foundry/{media_set_rid}/{branch}/{media_item_rid}#schema={}",
        schema.as_str()
    )
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn is_valid_media_reference_accepts_canonical_foundry_json() {
        let mr = json!({
            "mediaSetRid":  "ri.foundry.main.media_set.018f0000-aaaa-bbbb-cccc-000000000001",
            "mediaItemRid": "ri.foundry.main.media_item.018f0000-aaaa-bbbb-cccc-000000000002",
            "branch":       "main",
            "schema":       "IMAGE"
        })
        .to_string();
        let result = evaluate(
            MediaExpressionKind::IsValidMediaReference,
            &json!({ "input": mr }),
        )
        .unwrap();
        assert_eq!(result, Value::Bool(true));
    }

    #[test]
    fn is_valid_media_reference_rejects_arbitrary_json() {
        let result = evaluate(
            MediaExpressionKind::IsValidMediaReference,
            &json!({ "input": "{\"foo\":42}" }),
        )
        .unwrap();
        assert_eq!(result, Value::Bool(false));
    }

    #[test]
    fn construct_delegated_media_gid_emits_canonical_string() {
        let result = evaluate(
            MediaExpressionKind::ConstructDelegatedMediaGid,
            &json!({
                "media_set_rid":  "ri.foundry.main.media_set.set-1",
                "media_item_rid": "ri.foundry.main.media_item.item-1",
                "branch":         "main",
                "schema":         "IMAGE"
            }),
        )
        .unwrap();
        assert_eq!(
            result,
            Value::String(
                "gid:foundry/ri.foundry.main.media_set.set-1/main/ri.foundry.main.media_item.item-1\
                 #schema=IMAGE"
                    .to_string()
            )
        );
    }

    #[test]
    fn construct_delegated_media_gid_rejects_bad_rids() {
        let errs = validate(
            MediaExpressionKind::ConstructDelegatedMediaGid,
            &json!({
                "media_set_rid":  "not-a-rid",
                "media_item_rid": "ri.foundry.main.media_item.x",
                "schema":         "IMAGE"
            }),
        );
        assert!(errs.iter().any(|e| e.contains("media_set_rid")), "{errs:?}");
    }
}
