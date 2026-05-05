//! H6 — "Upload media" action-type scaffolding.
//!
//! Foundry's `Upload media.md` doc describes a Foundry-style action
//! that lets an operator pick a file in the action form and persists
//! it to a backing media set on submit (deferred upload — orphaned
//! files are never created when the form is cancelled or fails).
//!
//! On the OpenFoundry side, an "Upload media" action is just an
//! `update_object` action whose input schema contains a single
//! `media_reference`-typed field, plus the convention that the
//! field's `default_value` carries the backing media set's RID
//! (the UI binds this to the [`MediaSetUploader`] component).
//!
//! This module exposes:
//!
//!   * [`build_upload_media_action_input`] — the canonical
//!     [`crate::models::action_type::ActionInputField`] for the file
//!     parameter, with the backing media-set RID baked into
//!     `default_value` so the UI knows which set to upload to.
//!   * [`MediaSetBacking`] / [`backing_warnings`] — counts the
//!     distinct backing sets across an input schema and emits a
//!     warning per the doc's "Multiple media sets … strongly
//!     discouraged" guidance.
//!   * [`MediaUploadPlaceholder`] / [`detect_pending_uploads`] — the
//!     placeholder shape the front-end submits when the user picked
//!     a file but the backing media set has not yet been written to;
//!     the action submit path resolves these into canonical
//!     [`core_models::MediaReference`] shapes before applying the
//!     edit (the deferred-upload-on-submit contract from the doc).

use std::collections::BTreeSet;

use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

use crate::models::action_type::ActionInputField;

/// Backing media set declared by an `Upload media` field. We pull the
/// RID out of `default_value.media_set_rid` (camel- or snake-case);
/// Foundry's UI writes it there when the user picks a backing set.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct MediaSetBacking {
    pub field_name: String,
    pub media_set_rid: String,
}

/// Build the canonical `ActionInputField` for an Upload-media action.
/// Wired by the UI scaffolder + by tests that pin the shape.
pub fn build_upload_media_action_input(
    field_name: impl Into<String>,
    display_name: impl Into<String>,
    backing_media_set_rid: impl Into<String>,
) -> ActionInputField {
    let backing = backing_media_set_rid.into();
    ActionInputField {
        name: field_name.into(),
        display_name: Some(display_name.into()),
        description: Some(
            "Upload media field. The selected file is persisted to the backing \
             media set on submit; canceled forms never create orphans."
                .into(),
        ),
        property_type: "media_reference".to_string(),
        required: true,
        // The `default_value` carries the backing-set RID so the UI
        // and the validator both know where the file should land.
        // Mirrors the Foundry "Capabilities" tab convention for
        // media-reference properties.
        default_value: Some(json!({ "media_set_rid": backing })),
        struct_fields: None,
    }
}

/// Walk every `media_reference` input on an action and pull out its
/// backing media-set RID. Used by `backing_warnings` and by the UI
/// authoring screen to render the "Backing set" column.
pub fn collect_media_set_backings(input_schema: &[ActionInputField]) -> Vec<MediaSetBacking> {
    let mut out = Vec::new();
    for field in input_schema {
        if field.property_type != "media_reference" {
            continue;
        }
        let Some(value) = field.default_value.as_ref().and_then(Value::as_object) else {
            continue;
        };
        let rid = value
            .get("mediaSetRid")
            .or_else(|| value.get("media_set_rid"))
            .and_then(Value::as_str)
            .map(str::trim)
            .filter(|v| !v.is_empty());
        if let Some(rid) = rid {
            out.push(MediaSetBacking {
                field_name: field.name.clone(),
                media_set_rid: rid.to_string(),
            });
        }
    }
    out
}

/// Authoring-time warnings produced by the action editor.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "code", rename_all = "SCREAMING_SNAKE_CASE")]
pub enum MediaBackingWarning {
    /// Per `Using media in the Ontology.md`:
    ///   "Multiple media sets backing a media reference property in the
    ///    Capabilities tab of an object type are strongly discouraged.
    ///    Media uploads in actions are not fully supported in this case."
    MultipleBackingSets {
        message: String,
        distinct_sets: Vec<String>,
    },
    /// Per `Upload media.md`: "media reference list properties are not
    /// supported on an object" — surface as a warning so the authoring
    /// UI can guard the operator earlier than submit.
    MediaReferenceListNotSupported { field_name: String },
}

/// Run the doc-driven warnings against an action's input schema.
/// Returns the list verbatim so the UI can render them all together.
pub fn backing_warnings(input_schema: &[ActionInputField]) -> Vec<MediaBackingWarning> {
    let mut warnings = Vec::new();

    let backings = collect_media_set_backings(input_schema);
    let distinct: BTreeSet<&str> = backings
        .iter()
        .map(|b| b.media_set_rid.as_str())
        .collect();
    if distinct.len() > 1 {
        warnings.push(MediaBackingWarning::MultipleBackingSets {
            message: "Multiple media sets backing a single Upload-media action are \
                       strongly discouraged (Foundry: Using media in the Ontology). \
                       Uploads in actions are not fully supported in this case."
                .to_string(),
            distinct_sets: distinct.into_iter().map(String::from).collect(),
        });
    }

    // Foundry: media reference *list* properties are not supported.
    // We approximate "list" as a struct field whose `property_type`
    // chain involves `media_reference` repeated under an `array`.
    for field in input_schema {
        if field.property_type == "array"
            && field
                .struct_fields
                .as_ref()
                .map(|sub| sub.iter().any(|f| f.property_type == "media_reference"))
                .unwrap_or(false)
        {
            warnings.push(MediaBackingWarning::MediaReferenceListNotSupported {
                field_name: field.name.clone(),
            });
        }
    }

    warnings
}

/// What the front-end submits when the user picks a file but the
/// upload to the backing media set hasn't happened yet (the
/// "deferred" half of the doc's contract). The action submit path
/// resolves these into canonical [`core_models::MediaReference`]
/// shapes after a successful upload.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct MediaUploadPlaceholder {
    pub pending_upload: bool,
    pub media_set_rid: String,
    pub file_name: String,
    pub mime_type: String,
    /// Opaque token identifying the bytes the front-end staged
    /// (typically a `presigned_upload_url` SHA suffix). The submit
    /// handler exchanges this for a real `media_item_rid` via
    /// `media-sets-service` before applying the edit.
    pub blob_token: String,
}

impl MediaUploadPlaceholder {
    pub fn try_from_value(value: &Value) -> Option<Self> {
        let object = value.as_object()?;
        let pending = object
            .get("pendingUpload")
            .or_else(|| object.get("pending_upload"))
            .and_then(Value::as_bool)
            .unwrap_or(false);
        if !pending {
            return None;
        }
        let pull = |camel: &str, snake: &str| -> Option<String> {
            object
                .get(camel)
                .or_else(|| object.get(snake))
                .and_then(Value::as_str)
                .map(str::trim)
                .filter(|v| !v.is_empty())
                .map(str::to_string)
        };
        Some(Self {
            pending_upload: true,
            media_set_rid: pull("mediaSetRid", "media_set_rid")?,
            file_name: pull("fileName", "file_name")?,
            mime_type: pull("mimeType", "mime_type").unwrap_or_else(|| "application/octet-stream".into()),
            blob_token: pull("blobToken", "blob_token")?,
        })
    }
}

/// Surface every pending-upload placeholder in an input map. Used by
/// the action submit handler to know which fields to resolve before
/// applying the edit.
pub fn detect_pending_uploads(
    inputs: &serde_json::Map<String, Value>,
) -> Vec<(String, MediaUploadPlaceholder)> {
    inputs
        .iter()
        .filter_map(|(field_name, value)| {
            MediaUploadPlaceholder::try_from_value(value)
                .map(|placeholder| (field_name.clone(), placeholder))
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn field(
        name: &str,
        prop: &str,
        default: Option<Value>,
    ) -> ActionInputField {
        ActionInputField {
            name: name.into(),
            display_name: None,
            description: None,
            property_type: prop.into(),
            required: false,
            default_value: default,
            struct_fields: None,
        }
    }

    #[test]
    fn build_upload_media_action_input_pins_canonical_shape() {
        let f = build_upload_media_action_input(
            "photo",
            "Photo",
            "ri.foundry.main.media_set.aircraft",
        );
        assert_eq!(f.property_type, "media_reference");
        assert!(f.required);
        assert_eq!(
            f.default_value
                .as_ref()
                .and_then(|v| v.get("media_set_rid"))
                .and_then(Value::as_str),
            Some("ri.foundry.main.media_set.aircraft")
        );
    }

    #[test]
    fn single_backing_set_emits_no_warning() {
        let schema = vec![build_upload_media_action_input(
            "photo",
            "Photo",
            "ri.foundry.main.media_set.aircraft",
        )];
        assert!(backing_warnings(&schema).is_empty());
    }

    #[test]
    fn two_distinct_backing_sets_emit_strong_discouragement() {
        let schema = vec![
            build_upload_media_action_input("photo", "Photo", "ri.foundry.main.media_set.a"),
            build_upload_media_action_input("scan", "Scan", "ri.foundry.main.media_set.b"),
        ];
        let warnings = backing_warnings(&schema);
        assert_eq!(warnings.len(), 1);
        match &warnings[0] {
            MediaBackingWarning::MultipleBackingSets { distinct_sets, .. } => {
                assert_eq!(distinct_sets.len(), 2);
            }
            other => panic!("expected MultipleBackingSets, got {other:?}"),
        }
    }

    #[test]
    fn detects_pending_upload_placeholder() {
        let placeholder = json!({
            "pendingUpload": true,
            "mediaSetRid": "ri.foundry.main.media_set.x",
            "fileName": "skyline.png",
            "mimeType": "image/png",
            "blobToken": "abc123"
        });
        let parsed =
            MediaUploadPlaceholder::try_from_value(&placeholder).expect("should parse placeholder");
        assert_eq!(parsed.media_set_rid, "ri.foundry.main.media_set.x");
        assert_eq!(parsed.file_name, "skyline.png");
        assert_eq!(parsed.blob_token, "abc123");
    }

    #[test]
    fn detect_pending_uploads_finds_placeholder_inputs() {
        let mut inputs = serde_json::Map::new();
        inputs.insert(
            "photo".to_string(),
            json!({
                "pendingUpload": true,
                "mediaSetRid": "rid",
                "fileName": "x.png",
                "blobToken": "t"
            }),
        );
        inputs.insert("name".to_string(), json!("not a placeholder"));
        let detected = detect_pending_uploads(&inputs);
        assert_eq!(detected.len(), 1);
        assert_eq!(detected[0].0, "photo");
    }

    #[test]
    fn array_of_media_reference_is_warned_off() {
        let schema = vec![ActionInputField {
            name: "photos".into(),
            display_name: None,
            description: None,
            property_type: "array".into(),
            required: false,
            default_value: None,
            struct_fields: Some(vec![field("ref", "media_reference", None)]),
        }];
        let warnings = backing_warnings(&schema);
        assert!(matches!(
            warnings.first(),
            Some(MediaBackingWarning::MediaReferenceListNotSupported { field_name }) if field_name == "photos"
        ));
    }
}
