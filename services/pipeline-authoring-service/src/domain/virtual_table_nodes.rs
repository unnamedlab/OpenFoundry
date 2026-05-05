//! Virtual-table node kinds for the Pipeline Builder DAG (D1.1.9 P5).
//!
//! Foundry doc anchors:
//!   * `Virtual tables.md` § "Supported Foundry workflows" — pins
//!     which pipeline modes can use virtual tables (Snapshot,
//!     Incremental append-only; **not** Streaming, **not** Faster
//!     pipelines).
//!   * `Add a virtual table output.md` — the output node taxonomy +
//!     write-mode rules.
//!
//! Wire format mirrors `media_nodes.rs`: every node carries
//! `transform_type: String` + `config: serde_json::Value`. Two new
//! `transform_type` constants are added here:
//!
//!   * `virtual_table_input` — read from a registered virtual table.
//!   * `virtual_table_output` — write back into the source. The
//!     write_mode field is validated against the source × table-type
//!     capability matrix at compile time so a Managed Delta target
//!     cannot accept a write-mode = APPEND_ONLY config.

use serde::{Deserialize, Serialize};
use serde_json::Value;

// ---------------------------------------------------------------------------
// Transform-type discriminators.
// ---------------------------------------------------------------------------

pub const VIRTUAL_TABLE_INPUT: &str = "virtual_table_input";
pub const VIRTUAL_TABLE_OUTPUT: &str = "virtual_table_output";

pub const ALL_VIRTUAL_TABLE_TRANSFORM_TYPES: &[&str] =
    &[VIRTUAL_TABLE_INPUT, VIRTUAL_TABLE_OUTPUT];

pub fn is_virtual_table_transform_type(t: &str) -> bool {
    ALL_VIRTUAL_TABLE_TRANSFORM_TYPES.contains(&t)
}

// ---------------------------------------------------------------------------
// Configs.
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum IncrementalMode {
    /// Snapshot read — re-materialise the entire table on every build.
    None,
    /// Append-only incremental — read only rows added since the last
    /// build. Only valid when the source's capability matrix slot
    /// reports `incremental: true && append_only_supported: true`.
    AppendOnly,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct VirtualTableInputConfig {
    pub virtual_table_rid: String,
    #[serde(default = "default_incremental_mode")]
    pub incremental_mode: IncrementalMode,
}

fn default_incremental_mode() -> IncrementalMode {
    IncrementalMode::None
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum VirtualTableWriteMode {
    Snapshot,
    AppendOnly,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct VirtualTableOutputConfig {
    pub source_rid: String,
    pub locator: Value,
    pub write_mode: VirtualTableWriteMode,
}

// ---------------------------------------------------------------------------
// Capability slice (mirrors the Rust capability_matrix struct in
// `services/virtual-table-service/src/domain/capability_matrix.rs`).
// We keep a flat copy here so the validator does not have to depend on
// the virtual-table-service crate; the live API client fills it in
// from the row's `capabilities` JSONB.
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Copy, Default, Deserialize)]
pub struct VirtualTableCapabilitiesSlice {
    pub read: bool,
    pub write: bool,
    pub incremental: bool,
    pub append_only_supported: bool,
}

// ---------------------------------------------------------------------------
// Build-graph validation.
// ---------------------------------------------------------------------------

/// Pipeline mode resolved by the build orchestrator. The validator
/// rejects every (mode, virtual_table_node) combination that the
/// Foundry doc lists as "Not supported" under § "Supported Foundry
/// workflows".
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PipelineMode {
    Snapshot,
    /// Incremental builds (append-only) are blessed for virtual table
    /// inputs.
    IncrementalAppendOnly,
    /// Streaming pipelines are explicitly **not supported**.
    Streaming,
    /// Pipeline Builder "Faster pipelines" mode is explicitly **not
    /// supported** for virtual tables.
    Faster,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ValidationError {
    StreamingPipelineRejectsVirtualTable {
        node_kind: &'static str,
    },
    FasterPipelineRejectsVirtualTable {
        node_kind: &'static str,
    },
    OutputWriteNotSupported {
        source_rid: String,
        write_mode: VirtualTableWriteMode,
    },
    OutputAppendOnlyNotSupported {
        source_rid: String,
    },
    InputIncrementalNotSupported {
        virtual_table_rid: String,
    },
    MissingCapabilities {
        node_kind: &'static str,
    },
    InvalidConfig {
        node_kind: &'static str,
        error: String,
    },
}

impl ValidationError {
    pub fn code(&self) -> &'static str {
        match self {
            Self::StreamingPipelineRejectsVirtualTable { .. } => {
                "STREAMING_PIPELINE_NOT_SUPPORTED_FOR_VIRTUAL_TABLES"
            }
            Self::FasterPipelineRejectsVirtualTable { .. } => {
                "FASTER_PIPELINE_NOT_SUPPORTED_FOR_VIRTUAL_TABLES"
            }
            Self::OutputWriteNotSupported { .. } => "VIRTUAL_TABLE_OUTPUT_WRITE_NOT_SUPPORTED",
            Self::OutputAppendOnlyNotSupported { .. } => {
                "VIRTUAL_TABLE_OUTPUT_APPEND_ONLY_NOT_SUPPORTED"
            }
            Self::InputIncrementalNotSupported { .. } => {
                "VIRTUAL_TABLE_INPUT_INCREMENTAL_NOT_SUPPORTED"
            }
            Self::MissingCapabilities { .. } => "VIRTUAL_TABLE_CAPABILITIES_MISSING",
            Self::InvalidConfig { .. } => "VIRTUAL_TABLE_NODE_INVALID_CONFIG",
        }
    }
}

/// Validate a single virtual-table node against the published Foundry
/// matrix. Returns a single error (the highest-priority violation) so
/// the UI can surface it as a 422 toast.
pub fn validate_virtual_table_node(
    transform_type: &str,
    config: &Value,
    pipeline_mode: PipelineMode,
    capabilities: Option<VirtualTableCapabilitiesSlice>,
) -> std::result::Result<(), ValidationError> {
    // Doc § "Supported Foundry workflows" — Streaming + Faster
    // pipelines reject virtual tables.
    let node_kind: &'static str = match transform_type {
        VIRTUAL_TABLE_INPUT => "virtual_table_input",
        VIRTUAL_TABLE_OUTPUT => "virtual_table_output",
        // Not our node — the caller should not have invoked us, but
        // we treat this as a no-op to keep the validator additive.
        _ => return Ok(()),
    };

    if pipeline_mode == PipelineMode::Streaming {
        return Err(ValidationError::StreamingPipelineRejectsVirtualTable { node_kind });
    }
    if pipeline_mode == PipelineMode::Faster {
        return Err(ValidationError::FasterPipelineRejectsVirtualTable { node_kind });
    }

    match transform_type {
        VIRTUAL_TABLE_INPUT => {
            let cfg: VirtualTableInputConfig =
                serde_json::from_value(config.clone()).map_err(|e| ValidationError::InvalidConfig {
                    node_kind,
                    error: e.to_string(),
                })?;
            if matches!(cfg.incremental_mode, IncrementalMode::AppendOnly) {
                let caps = capabilities
                    .ok_or(ValidationError::MissingCapabilities { node_kind })?;
                if !caps.incremental || !caps.append_only_supported {
                    return Err(ValidationError::InputIncrementalNotSupported {
                        virtual_table_rid: cfg.virtual_table_rid,
                    });
                }
            }
            Ok(())
        }
        VIRTUAL_TABLE_OUTPUT => {
            let cfg: VirtualTableOutputConfig =
                serde_json::from_value(config.clone()).map_err(|e| ValidationError::InvalidConfig {
                    node_kind,
                    error: e.to_string(),
                })?;
            let caps = capabilities
                .ok_or(ValidationError::MissingCapabilities { node_kind })?;
            if !caps.write {
                return Err(ValidationError::OutputWriteNotSupported {
                    source_rid: cfg.source_rid,
                    write_mode: cfg.write_mode,
                });
            }
            if matches!(cfg.write_mode, VirtualTableWriteMode::AppendOnly)
                && !caps.append_only_supported
            {
                return Err(ValidationError::OutputAppendOnlyNotSupported {
                    source_rid: cfg.source_rid,
                });
            }
            Ok(())
        }
        _ => unreachable!(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn input_config(mode: &str) -> Value {
        json!({
            "virtual_table_rid": "ri.foundry.main.virtual-table.x",
            "incremental_mode": mode,
        })
    }

    fn output_config(mode: &str) -> Value {
        json!({
            "source_rid": "ri.source.x",
            "locator": { "kind": "tabular", "database": "db", "schema": "s", "table": "t" },
            "write_mode": mode,
        })
    }

    fn caps(read: bool, write: bool, incremental: bool, append_only: bool) -> VirtualTableCapabilitiesSlice {
        VirtualTableCapabilitiesSlice {
            read,
            write,
            incremental,
            append_only_supported: append_only,
        }
    }

    #[test]
    fn streaming_rejects_virtual_table_input() {
        let err = validate_virtual_table_node(
            VIRTUAL_TABLE_INPUT,
            &input_config("NONE"),
            PipelineMode::Streaming,
            Some(caps(true, false, false, false)),
        )
        .expect_err("must reject");
        assert_eq!(err.code(), "STREAMING_PIPELINE_NOT_SUPPORTED_FOR_VIRTUAL_TABLES");
    }

    #[test]
    fn faster_pipelines_rejects_virtual_table_output() {
        let err = validate_virtual_table_node(
            VIRTUAL_TABLE_OUTPUT,
            &output_config("SNAPSHOT"),
            PipelineMode::Faster,
            Some(caps(true, true, false, false)),
        )
        .expect_err("must reject");
        assert_eq!(err.code(), "FASTER_PIPELINE_NOT_SUPPORTED_FOR_VIRTUAL_TABLES");
    }

    #[test]
    fn output_append_only_requires_capability() {
        // Source supports write but not append-only — APPEND_ONLY mode is rejected.
        let err = validate_virtual_table_node(
            VIRTUAL_TABLE_OUTPUT,
            &output_config("APPEND_ONLY"),
            PipelineMode::Snapshot,
            Some(caps(true, true, true, false)),
        )
        .expect_err("must reject");
        assert_eq!(err.code(), "VIRTUAL_TABLE_OUTPUT_APPEND_ONLY_NOT_SUPPORTED");
    }

    #[test]
    fn output_snapshot_requires_write_capability() {
        let err = validate_virtual_table_node(
            VIRTUAL_TABLE_OUTPUT,
            &output_config("SNAPSHOT"),
            PipelineMode::Snapshot,
            Some(caps(true, false, false, false)),
        )
        .expect_err("must reject");
        assert_eq!(err.code(), "VIRTUAL_TABLE_OUTPUT_WRITE_NOT_SUPPORTED");
    }

    #[test]
    fn output_snapshot_succeeds_when_write_supported() {
        validate_virtual_table_node(
            VIRTUAL_TABLE_OUTPUT,
            &output_config("SNAPSHOT"),
            PipelineMode::Snapshot,
            Some(caps(true, true, false, false)),
        )
        .expect("should accept");
    }

    #[test]
    fn input_incremental_requires_capability() {
        let err = validate_virtual_table_node(
            VIRTUAL_TABLE_INPUT,
            &input_config("APPEND_ONLY"),
            PipelineMode::IncrementalAppendOnly,
            Some(caps(true, false, false, false)),
        )
        .expect_err("must reject");
        assert_eq!(err.code(), "VIRTUAL_TABLE_INPUT_INCREMENTAL_NOT_SUPPORTED");
    }

    #[test]
    fn input_incremental_accepted_when_capability_present() {
        validate_virtual_table_node(
            VIRTUAL_TABLE_INPUT,
            &input_config("APPEND_ONLY"),
            PipelineMode::IncrementalAppendOnly,
            Some(caps(true, false, true, true)),
        )
        .expect("should accept");
    }

    #[test]
    fn unknown_transform_type_is_a_no_op() {
        validate_virtual_table_node(
            "media_set_input",
            &json!({}),
            PipelineMode::Snapshot,
            None,
        )
        .expect("must not reject other transform types");
    }
}
