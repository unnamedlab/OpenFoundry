//! Media-set node types for the Pipeline Builder DAG.
//!
//! Foundry sources:
//! * `Add a media set output.md`         — output node configurations.
//! * `Transform media.md`                 — `MediaTransform` taxonomy.
//! * `Convert media set to table rows.md` — items → dataset rows.
//! * `Get media references (datasets).md` — dataset rows → media refs.
//!
//! Wire format
//! -----------
//! Pipeline nodes carry `transform_type: String` + `config:
//! serde_json::Value` (see `crate::models::pipeline::PipelineNode`).
//! The new node kinds therefore plug into the existing dispatch via the
//! [`transform_type`] constants below; their per-kind configs live in
//! the `config` JSON object and are decoded through this module's typed
//! structs.
//!
//! Type validation
//! ---------------
//! Every kind exposes:
//! * an `accepted_input_schemas()` set — the
//!   [`core_models::MediaSetSchema`] values the node can read.
//! * an `output_kind()` — what the node produces (`MediaSetOutput`,
//!   `DatasetOutput` or `Sideeffect`). The compiler uses this to
//!   reject DAGs where, e.g., a `transcribe_audio` node is wired to
//!   downstream nodes expecting an IMAGE input.
//!
//! Execution
//! ---------
//! P1.4 deliberately keeps the runtime as a stub
//! ([`crate::domain::engine::runtime::execute_media_node_stub`]). The
//! actual transformation work lives in the future
//! `media-transform-runtime` service that this stub will later POST
//! against. The wire-format contract this module pins down is
//! sufficient for the authoring side (validation + palette + lineage)
//! to ship without that runtime.

use std::collections::HashSet;

use core_models::MediaSetSchema;
use serde::{Deserialize, Serialize};
use serde_json::Value;

// ---------------------------------------------------------------------------
// Transform-type discriminators (the strings that appear on
// `PipelineNode.transform_type`).
// ---------------------------------------------------------------------------

pub const MEDIA_SET_INPUT: &str = "media_set_input";
pub const MEDIA_SET_OUTPUT: &str = "media_set_output";
pub const MEDIA_TRANSFORM: &str = "media_transform";
pub const CONVERT_MEDIA_SET_TO_TABLE_ROWS: &str = "convert_media_set_to_table_rows";
pub const GET_MEDIA_REFERENCES: &str = "get_media_references";

/// All transform-type strings owned by this module. The pipeline
/// validator + engine dispatch use this to short-circuit when a node's
/// `transform_type` is "ours".
pub const ALL_MEDIA_TRANSFORM_TYPES: &[&str] = &[
    MEDIA_SET_INPUT,
    MEDIA_SET_OUTPUT,
    MEDIA_TRANSFORM,
    CONVERT_MEDIA_SET_TO_TABLE_ROWS,
    GET_MEDIA_REFERENCES,
];

pub fn is_media_transform_type(t: &str) -> bool {
    ALL_MEDIA_TRANSFORM_TYPES.contains(&t)
}

// ---------------------------------------------------------------------------
// Node configs
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct MediaSetInputConfig {
    pub media_set_rid: String,
    /// Defaults to `"main"`.
    #[serde(default = "default_branch")]
    pub branch: String,
    /// Optional path prefix to read only a subset of items.
    #[serde(default)]
    pub path_prefix: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct MediaSetOutputConfig {
    /// Either bind to an existing media set …
    #[serde(default)]
    pub media_set_rid: Option<String>,
    /// … or create one. Both forms are valid; exactly one must be set.
    #[serde(default)]
    pub create_if_missing: Option<CreateMediaSetSpec>,
    #[serde(default = "default_branch")]
    pub branch: String,
    /// Foundry write modes — `replace` and `modify` for transactional
    /// targets, `modify` only for transactionless ones (per
    /// "Advanced media set settings" docs).
    #[serde(default)]
    pub write_mode: Option<WriteMode>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct CreateMediaSetSpec {
    pub project_rid: String,
    pub name: String,
    pub schema: MediaSetSchema,
    #[serde(default)]
    pub allowed_mime_types: Vec<String>,
    /// Defaults to `TRANSACTIONLESS` to match the Foundry default.
    #[serde(default)]
    pub transaction_policy: Option<TransactionPolicyDto>,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum TransactionPolicyDto {
    Transactionless,
    Transactional,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum WriteMode {
    Replace,
    Modify,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct MediaTransformConfig {
    pub kind: MediaTransformKind,
    /// Kind-specific parameters (e.g. `{ "width": 256, "height": 256 }`
    /// for `resize`). Validation checks the required keys per kind.
    #[serde(default)]
    pub params: Value,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum MediaTransformKind {
    ExtractTextOcr,
    Resize,
    Rotate,
    Crop,
    TranscribeAudio,
    GenerateEmbedding,
    RenderPdfPage,
    ExtractLayoutAware,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ConvertMediaSetToTableRowsConfig {
    pub source_media_set_rid: String,
    #[serde(default = "default_branch")]
    pub branch: String,
    /// Whether to surface the media reference (`mediaSetRid` /
    /// `mediaItemRid` JSON) as one of the output columns.
    #[serde(default = "default_true")]
    pub include_media_reference: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct GetMediaReferencesConfig {
    pub source_dataset_id: uuid::Uuid,
    pub target_media_set_rid: String,
    /// Optional MIME-type override. When unset the executor sniffs the
    /// underlying file (Foundry parity with the
    /// "Forces the mime type" knob in the docs).
    #[serde(default)]
    pub force_mime_type: Option<String>,
}

fn default_branch() -> String {
    "main".to_string()
}

fn default_true() -> bool {
    true
}

// ---------------------------------------------------------------------------
// Output classification (what does a node produce downstream?)
// ---------------------------------------------------------------------------

/// The compiler uses this to know what the next node can consume:
///
/// * `MediaItems` → the next node may be another `MediaTransform` or a
///   `MediaSetOutput`.
/// * `DatasetRows` → the next node consumes tabular data (e.g.
///   `ConvertMediaSetToTableRows` produces this; SQL nodes can read it).
/// * `Sideeffect` → terminal nodes that don't feed downstream nodes
///   (e.g. `MediaSetOutput`).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum MediaNodeOutput {
    MediaItems,
    DatasetRows,
    Sideeffect,
}

impl MediaTransformKind {
    /// Schemas the transform can read. Empty set = "any media schema".
    pub fn accepted_input_schemas(self) -> HashSet<MediaSetSchema> {
        use MediaSetSchema::*;
        match self {
            Self::ExtractTextOcr => [Document, Image].into(),
            Self::Resize | Self::Rotate | Self::Crop => [Image].into(),
            Self::TranscribeAudio => [Audio, Video].into(),
            Self::GenerateEmbedding => HashSet::new(),
            Self::RenderPdfPage => [Document].into(),
            Self::ExtractLayoutAware => [Document].into(),
        }
    }

    /// Whether the transform writes back into a media set
    /// (`MediaItems`) or extracts to dataset rows (`DatasetRows`).
    pub fn output(self) -> MediaNodeOutput {
        match self {
            // OCR / transcription / embedding emit text or vectors that
            // belong in tabular data, not media items.
            Self::ExtractTextOcr
            | Self::TranscribeAudio
            | Self::GenerateEmbedding
            | Self::ExtractLayoutAware => MediaNodeOutput::DatasetRows,
            // Pixel + page-render transforms produce derived media.
            Self::Resize | Self::Rotate | Self::Crop | Self::RenderPdfPage => {
                MediaNodeOutput::MediaItems
            }
        }
    }

    /// Required keys in the `params` JSON object — checked by
    /// [`validate_media_node`].
    pub fn required_params(self) -> &'static [&'static str] {
        match self {
            Self::Resize => &["width", "height"],
            Self::Rotate => &["degrees"],
            Self::Crop => &["x", "y", "width", "height"],
            Self::RenderPdfPage => &["page"],
            Self::TranscribeAudio
            | Self::ExtractTextOcr
            | Self::GenerateEmbedding
            | Self::ExtractLayoutAware => &[],
        }
    }
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

/// Decode + validate the per-kind config of a media-typed node.
///
/// Returns the list of human-readable errors (empty = valid). Callers
/// in `crate::domain::compiler` aggregate these into the pipeline-level
/// validation response.
///
/// `transform_type` is read off `PipelineNode.transform_type`; `config`
/// is read off `PipelineNode.config`. Non-media transform types return
/// an empty error list (they are not our concern).
pub fn validate_media_node(transform_type: &str, config: &Value) -> Vec<String> {
    let mut errors = Vec::new();
    match transform_type {
        MEDIA_SET_INPUT => match serde_json::from_value::<MediaSetInputConfig>(config.clone()) {
            Ok(cfg) => {
                if !cfg.media_set_rid.starts_with("ri.foundry.main.media_set.") {
                    errors.push(format!(
                        "media_set_input.media_set_rid `{}` is not a Foundry media-set RID",
                        cfg.media_set_rid
                    ));
                }
                if cfg.branch.trim().is_empty() {
                    errors.push("media_set_input.branch must not be empty".into());
                }
            }
            Err(err) => errors.push(format!("media_set_input config: {err}")),
        },
        MEDIA_SET_OUTPUT => {
            match serde_json::from_value::<MediaSetOutputConfig>(config.clone()) {
                Ok(cfg) => {
                    let bound = cfg.media_set_rid.is_some();
                    let creating = cfg.create_if_missing.is_some();
                    if bound == creating {
                        errors.push(
                            "media_set_output requires exactly one of `media_set_rid` or \
                             `create_if_missing`"
                                .into(),
                        );
                    }
                    if let Some(spec) = &cfg.create_if_missing {
                        if spec.project_rid.trim().is_empty() {
                            errors.push(
                                "media_set_output.create_if_missing.project_rid required".into(),
                            );
                        }
                        if spec.name.trim().is_empty() {
                            errors.push("media_set_output.create_if_missing.name required".into());
                        }
                    }
                    if let (Some(WriteMode::Replace), Some(TransactionPolicyDto::Transactionless)) = (
                        cfg.write_mode,
                        cfg.create_if_missing
                            .as_ref()
                            .and_then(|s| s.transaction_policy),
                    ) {
                        // Foundry: TRANSACTIONLESS sets only support
                        // `modify` (see *Advanced media set settings*).
                        errors.push(
                            "write_mode=replace requires a TRANSACTIONAL media set (per \
                             Foundry Advanced media set settings docs)"
                                .into(),
                        );
                    }
                }
                Err(err) => errors.push(format!("media_set_output config: {err}")),
            }
        }
        MEDIA_TRANSFORM => match serde_json::from_value::<MediaTransformConfig>(config.clone()) {
            Ok(cfg) => {
                let params = cfg.params.as_object();
                for required in cfg.kind.required_params() {
                    if !params.map(|p| p.contains_key(*required)).unwrap_or(false) {
                        errors.push(format!(
                            "media_transform.{:?}.params is missing required key `{}`",
                            cfg.kind, required
                        ));
                    }
                }
            }
            Err(err) => errors.push(format!("media_transform config: {err}")),
        },
        CONVERT_MEDIA_SET_TO_TABLE_ROWS => {
            match serde_json::from_value::<ConvertMediaSetToTableRowsConfig>(config.clone()) {
                Ok(cfg) => {
                    if !cfg
                        .source_media_set_rid
                        .starts_with("ri.foundry.main.media_set.")
                    {
                        errors.push(format!(
                            "convert_media_set_to_table_rows.source_media_set_rid `{}` is not a \
                             Foundry media-set RID",
                            cfg.source_media_set_rid
                        ));
                    }
                }
                Err(err) => errors.push(format!("convert_media_set_to_table_rows config: {err}")),
            }
        }
        GET_MEDIA_REFERENCES => {
            match serde_json::from_value::<GetMediaReferencesConfig>(config.clone()) {
                Ok(cfg) => {
                    if !cfg
                        .target_media_set_rid
                        .starts_with("ri.foundry.main.media_set.")
                    {
                        errors.push(format!(
                            "get_media_references.target_media_set_rid `{}` is not a Foundry \
                             media-set RID",
                            cfg.target_media_set_rid
                        ));
                    }
                }
                Err(err) => errors.push(format!("get_media_references config: {err}")),
            }
        }
        _ => {} // non-media node: not our concern
    }
    errors
}

// ---------------------------------------------------------------------------
// Node palette
// ---------------------------------------------------------------------------

/// JSON descriptor consumed by the Pipeline Builder UI to render the
/// "add node" sidebar. Stable wire shape — keep keys in sync with
/// `apps/web/src/lib/pipeline/node-palette.ts` when the UI lands.
pub fn node_palette() -> Value {
    serde_json::json!([
        {
            "type": MEDIA_SET_INPUT,
            "label": "Media set input",
            "category": "media",
            "input_count":  0,
            "output_count": 1,
            "output_kind":  "media_items",
            "config_schema": {
                "fields": [
                    { "name": "media_set_rid", "kind": "string", "required": true },
                    { "name": "branch",        "kind": "string", "default": "main" },
                    { "name": "path_prefix",   "kind": "string", "required": false }
                ]
            }
        },
        {
            "type": MEDIA_SET_OUTPUT,
            "label": "Media set output",
            "category": "media",
            "input_count":  1,
            "output_count": 0,
            "output_kind":  "sideeffect",
            "config_schema": {
                "fields": [
                    { "name": "media_set_rid",     "kind": "string",  "required": false },
                    { "name": "create_if_missing", "kind": "object",  "required": false },
                    { "name": "branch",            "kind": "string",  "default": "main" },
                    { "name": "write_mode",        "kind": "enum",    "values": ["replace", "modify"] }
                ]
            }
        },
        {
            "type": MEDIA_TRANSFORM,
            "label": "Transform media",
            "category": "media",
            "input_count":  1,
            "output_count": 1,
            "kinds": [
                { "id": "extract_text_ocr",     "input_schemas": ["DOCUMENT", "IMAGE"], "output_kind": "dataset_rows" },
                { "id": "resize",               "input_schemas": ["IMAGE"],             "output_kind": "media_items", "params": ["width", "height"] },
                { "id": "rotate",               "input_schemas": ["IMAGE"],             "output_kind": "media_items", "params": ["degrees"] },
                { "id": "crop",                 "input_schemas": ["IMAGE"],             "output_kind": "media_items", "params": ["x", "y", "width", "height"] },
                { "id": "transcribe_audio",     "input_schemas": ["AUDIO", "VIDEO"],    "output_kind": "dataset_rows" },
                { "id": "generate_embedding",   "input_schemas": [],                    "output_kind": "dataset_rows" },
                { "id": "render_pdf_page",      "input_schemas": ["DOCUMENT"],          "output_kind": "media_items", "params": ["page"] },
                { "id": "extract_layout_aware", "input_schemas": ["DOCUMENT"],          "output_kind": "dataset_rows" }
            ]
        },
        {
            "type": CONVERT_MEDIA_SET_TO_TABLE_ROWS,
            "label": "Convert media set to table rows",
            "category": "media",
            "input_count":  1,
            "output_count": 1,
            "output_kind":  "dataset_rows",
            "config_schema": {
                "fields": [
                    { "name": "source_media_set_rid",   "kind": "string", "required": true },
                    { "name": "branch",                 "kind": "string", "default": "main" },
                    { "name": "include_media_reference","kind": "boolean", "default": true }
                ]
            }
        },
        {
            "type": GET_MEDIA_REFERENCES,
            "label": "Get media references (dataset)",
            "category": "media",
            "input_count":  1,
            "output_count": 1,
            "output_kind":  "dataset_rows",
            "config_schema": {
                "fields": [
                    { "name": "source_dataset_id",      "kind": "uuid",   "required": true },
                    { "name": "target_media_set_rid",   "kind": "string", "required": true },
                    { "name": "force_mime_type",        "kind": "string", "required": false }
                ]
            }
        }
    ])
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn palette_lists_all_media_node_types() {
        let palette = node_palette();
        let arr = palette.as_array().expect("palette is an array");
        let types: HashSet<&str> = arr
            .iter()
            .map(|node| node["type"].as_str().unwrap())
            .collect();
        for t in ALL_MEDIA_TRANSFORM_TYPES {
            assert!(types.contains(t), "palette missing entry for {t}");
        }
    }

    #[test]
    fn validate_media_set_input_rejects_non_rid() {
        let errs = validate_media_node(
            MEDIA_SET_INPUT,
            &json!({ "media_set_rid": "not-a-rid", "branch": "main" }),
        );
        assert_eq!(errs.len(), 1, "{errs:?}");
        assert!(errs[0].contains("not a Foundry media-set RID"));
    }

    #[test]
    fn media_transform_resize_requires_width_height() {
        let missing_height = validate_media_node(
            MEDIA_TRANSFORM,
            &json!({ "kind": "resize", "params": { "width": 256 } }),
        );
        assert!(
            missing_height.iter().any(|e| e.contains("`height`")),
            "{missing_height:?}"
        );

        let ok = validate_media_node(
            MEDIA_TRANSFORM,
            &json!({ "kind": "resize", "params": { "width": 256, "height": 256 } }),
        );
        assert!(ok.is_empty(), "{ok:?}");
    }
}
