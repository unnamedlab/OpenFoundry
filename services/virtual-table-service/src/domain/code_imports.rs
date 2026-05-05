//! Code Repositories integration toggles for virtual tables (D1.1.9 P6).
//!
//! Foundry doc § "Virtual tables in Code Repositories" prescribes two
//! source-level toggles:
//!
//!   * **Code imports** — when `code_imports_enabled = true`, the
//!     source can be imported into a code repository and used as a
//!     `transforms.api.Input` for a virtual table.
//!   * **Export controls** — caps the markings/organisations the
//!     virtual-table inputs can carry into a Python Transform run.
//!     Stored as a JSONB blob on the link row so the Cedar policy
//!     layer can read it directly.
//!
//! The build-time validator in
//! `services/code-repository-review-service` (or the Python SDK in
//! `sdks/python/openfoundry_transforms/virtual_tables.py`) calls
//! [`validate_code_import`] before allowing the build graph to
//! resolve a virtual-table input. Three rejection codes mirror the
//! Foundry doc § "Limitations":
//!
//!   * `SOURCE_NOT_IMPORTABLE_INTO_CODE` — the toggle is off.
//!   * `EXPORT_CONTROL_VIOLATION` — the input's markings exceed the
//!     allow-list.
//!   * `VIRTUAL_TABLE_USE_EXTERNAL_SYSTEMS_INCOMPAT` — the transform
//!     mixes `@use_external_systems` with a virtual-table input.

use std::collections::HashSet;

use serde::{Deserialize, Serialize};
use serde_json::{Value, json};

use crate::AppState;
use crate::domain::audit;
use crate::models::virtual_table::VirtualTableSourceLink;

// ---------------------------------------------------------------------------
// Endpoint payloads.
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, Deserialize)]
pub struct ToggleCodeImportsRequest {
    pub enabled: bool,
}

/// Body of `POST /v1/sources/{rid}/export-controls`. The two lists are
/// allow-lists; an empty list is read as "no constraint" so a tenant
/// can opt-in to code imports without naming individual markings yet.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct ExportControls {
    #[serde(default)]
    pub allowed_markings: Vec<String>,
    #[serde(default)]
    pub allowed_organizations: Vec<String>,
}

impl ExportControls {
    pub fn is_constrained(&self) -> bool {
        !self.allowed_markings.is_empty() || !self.allowed_organizations.is_empty()
    }

    pub fn to_value(&self) -> Value {
        serde_json::to_value(self).unwrap_or(Value::Object(Default::default()))
    }

    pub fn from_value(value: &Value) -> Self {
        serde_json::from_value(value.clone()).unwrap_or_default()
    }
}

// ---------------------------------------------------------------------------
// Validator.
// ---------------------------------------------------------------------------

/// Stable error codes surfaced in the build-time review payload.
pub mod error_code {
    pub const SOURCE_NOT_IMPORTABLE_INTO_CODE: &str = "SOURCE_NOT_IMPORTABLE_INTO_CODE";
    pub const EXPORT_CONTROL_VIOLATION: &str = "EXPORT_CONTROL_VIOLATION";
    pub const USE_EXTERNAL_SYSTEMS_INCOMPAT: &str =
        "VIRTUAL_TABLE_USE_EXTERNAL_SYSTEMS_INCOMPAT";
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
#[serde(tag = "code", rename_all = "SCREAMING_SNAKE_CASE")]
pub enum CodeImportRejection {
    SourceNotImportableIntoCode {
        source_rid: String,
    },
    ExportControlViolation {
        offending_markings: Vec<String>,
        allowed_markings: Vec<String>,
    },
    OrganizationNotAllowed {
        offending_organization: String,
        allowed_organizations: Vec<String>,
    },
    UseExternalSystemsIncompat {
        virtual_table_rid: String,
    },
}

impl CodeImportRejection {
    pub fn code(&self) -> &'static str {
        match self {
            Self::SourceNotImportableIntoCode { .. } => error_code::SOURCE_NOT_IMPORTABLE_INTO_CODE,
            Self::ExportControlViolation { .. }
            | Self::OrganizationNotAllowed { .. } => error_code::EXPORT_CONTROL_VIOLATION,
            Self::UseExternalSystemsIncompat { .. } => error_code::USE_EXTERNAL_SYSTEMS_INCOMPAT,
        }
    }
}

/// Snapshot of the import-time inputs the validator inspects. The
/// fields are kept narrow — anything richer is the Cedar layer's
/// responsibility, not this validator's.
#[derive(Debug, Clone, Default)]
pub struct CodeImportContext<'a> {
    pub source_rid: &'a str,
    pub virtual_table_rid: &'a str,
    pub virtual_table_markings: &'a [String],
    pub virtual_table_organization: Option<&'a str>,
    pub transform_uses_external_systems: bool,
}

/// Validate a single virtual-table input against the source's
/// code-imports configuration. Returns the first rejection so the UI
/// can surface a single 422 toast; the caller can re-run for batch
/// validation.
pub fn validate_code_import(
    link: &VirtualTableSourceLink,
    ctx: CodeImportContext<'_>,
) -> std::result::Result<(), CodeImportRejection> {
    if !link.code_imports_enabled {
        return Err(CodeImportRejection::SourceNotImportableIntoCode {
            source_rid: ctx.source_rid.to_string(),
        });
    }

    let controls = ExportControls::from_value(&link.export_controls);

    if !controls.allowed_markings.is_empty() {
        let allowed: HashSet<&str> = controls
            .allowed_markings
            .iter()
            .map(String::as_str)
            .collect();
        let offending: Vec<String> = ctx
            .virtual_table_markings
            .iter()
            .filter(|m| !allowed.contains(m.as_str()))
            .cloned()
            .collect();
        if !offending.is_empty() {
            return Err(CodeImportRejection::ExportControlViolation {
                offending_markings: offending,
                allowed_markings: controls.allowed_markings.clone(),
            });
        }
    }

    if !controls.allowed_organizations.is_empty() {
        if let Some(org) = ctx.virtual_table_organization {
            let allowed: HashSet<&str> = controls
                .allowed_organizations
                .iter()
                .map(String::as_str)
                .collect();
            if !allowed.contains(org) {
                return Err(CodeImportRejection::OrganizationNotAllowed {
                    offending_organization: org.to_string(),
                    allowed_organizations: controls.allowed_organizations.clone(),
                });
            }
        }
    }

    if ctx.transform_uses_external_systems {
        return Err(CodeImportRejection::UseExternalSystemsIncompat {
            virtual_table_rid: ctx.virtual_table_rid.to_string(),
        });
    }

    Ok(())
}

// ---------------------------------------------------------------------------
// Persistence helpers.
// ---------------------------------------------------------------------------

#[derive(Debug, thiserror::Error)]
pub enum CodeImportError {
    #[error("source not registered for virtual tables: {0}")]
    NotConfigured(String),
    #[error("database error: {0}")]
    Database(#[from] sqlx::Error),
}

pub type Result<T> = std::result::Result<T, CodeImportError>;

pub async fn set_code_imports_enabled(
    state: &AppState,
    source_rid: &str,
    actor_id: Option<&str>,
    body: ToggleCodeImportsRequest,
) -> Result<VirtualTableSourceLink> {
    let row: Option<VirtualTableSourceLink> = sqlx::query_as(
        r#"UPDATE virtual_table_sources_link
            SET code_imports_enabled = $1, updated_at = NOW()
            WHERE source_rid = $2
            RETURNING *"#,
    )
    .bind(body.enabled)
    .bind(source_rid)
    .fetch_optional(&state.db)
    .await?;
    let row = row.ok_or_else(|| CodeImportError::NotConfigured(source_rid.to_string()))?;

    audit::record(
        &state.db,
        Some(source_rid),
        None,
        if body.enabled {
            "virtual_table.code_imports_enabled"
        } else {
            "virtual_table.code_imports_disabled"
        },
        actor_id,
        json!({ "enabled": body.enabled }),
    )
    .await;

    Ok(row)
}

pub async fn set_export_controls(
    state: &AppState,
    source_rid: &str,
    actor_id: Option<&str>,
    body: ExportControls,
) -> Result<VirtualTableSourceLink> {
    let value = body.to_value();
    let row: Option<VirtualTableSourceLink> = sqlx::query_as(
        r#"UPDATE virtual_table_sources_link
            SET export_controls = $1::jsonb, updated_at = NOW()
            WHERE source_rid = $2
            RETURNING *"#,
    )
    .bind(&value)
    .bind(source_rid)
    .fetch_optional(&state.db)
    .await?;
    let row = row.ok_or_else(|| CodeImportError::NotConfigured(source_rid.to_string()))?;

    audit::record(
        &state.db,
        Some(source_rid),
        None,
        "virtual_table.export_controls_updated",
        actor_id,
        json!({
            "allowed_markings": body.allowed_markings,
            "allowed_organizations": body.allowed_organizations,
            "constrained": body.is_constrained(),
        }),
    )
    .await;

    Ok(row)
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::Utc;

    fn link(
        code_imports_enabled: bool,
        controls: ExportControls,
    ) -> VirtualTableSourceLink {
        VirtualTableSourceLink {
            source_rid: "ri.source.bq".into(),
            provider: "BIGQUERY".into(),
            virtual_tables_enabled: true,
            code_imports_enabled,
            export_controls: controls.to_value(),
            auto_register_project_rid: None,
            auto_register_enabled: false,
            auto_register_interval_seconds: None,
            auto_register_tag_filters: serde_json::json!([]),
            iceberg_catalog_kind: None,
            iceberg_catalog_config: None,
            created_at: Utc::now(),
            updated_at: Utc::now(),
        }
    }

    fn ctx<'a>(
        source_rid: &'a str,
        rid: &'a str,
        markings: &'a [String],
        org: Option<&'a str>,
        uses_external_systems: bool,
    ) -> CodeImportContext<'a> {
        CodeImportContext {
            source_rid,
            virtual_table_rid: rid,
            virtual_table_markings: markings,
            virtual_table_organization: org,
            transform_uses_external_systems: uses_external_systems,
        }
    }

    #[test]
    fn rejects_when_code_imports_disabled() {
        let l = link(false, ExportControls::default());
        let markings: Vec<String> = vec![];
        let err = validate_code_import(&l, ctx("s", "vt", &markings, None, false))
            .expect_err("must reject");
        assert_eq!(err.code(), error_code::SOURCE_NOT_IMPORTABLE_INTO_CODE);
    }

    #[test]
    fn accepts_when_code_imports_enabled_and_no_constraints() {
        let l = link(true, ExportControls::default());
        let markings: Vec<String> = vec!["pii".into()];
        validate_code_import(&l, ctx("s", "vt", &markings, None, false))
            .expect("must accept");
    }

    #[test]
    fn rejects_marking_not_in_allow_list() {
        let l = link(
            true,
            ExportControls {
                allowed_markings: vec!["public".into(), "internal".into()],
                allowed_organizations: vec![],
            },
        );
        let markings: Vec<String> = vec!["pii".into(), "internal".into()];
        let err = validate_code_import(&l, ctx("s", "vt", &markings, None, false))
            .expect_err("must reject");
        assert_eq!(err.code(), error_code::EXPORT_CONTROL_VIOLATION);
        if let CodeImportRejection::ExportControlViolation { offending_markings, .. } = err {
            assert_eq!(offending_markings, vec!["pii".to_string()]);
        } else {
            panic!("unexpected variant");
        }
    }

    #[test]
    fn accepts_when_all_markings_in_allow_list() {
        let l = link(
            true,
            ExportControls {
                allowed_markings: vec!["public".into(), "internal".into()],
                allowed_organizations: vec![],
            },
        );
        let markings: Vec<String> = vec!["public".into(), "internal".into()];
        validate_code_import(&l, ctx("s", "vt", &markings, None, false))
            .expect("must accept");
    }

    #[test]
    fn rejects_organization_not_in_allow_list() {
        let l = link(
            true,
            ExportControls {
                allowed_markings: vec![],
                allowed_organizations: vec!["acme".into()],
            },
        );
        let markings: Vec<String> = vec![];
        let err = validate_code_import(&l, ctx("s", "vt", &markings, Some("nemesis"), false))
            .expect_err("must reject");
        assert_eq!(err.code(), error_code::EXPORT_CONTROL_VIOLATION);
    }

    #[test]
    fn rejects_use_external_systems_combined_with_virtual_table() {
        let l = link(true, ExportControls::default());
        let markings: Vec<String> = vec![];
        let err = validate_code_import(&l, ctx("s", "vt", &markings, None, true))
            .expect_err("must reject");
        assert_eq!(err.code(), error_code::USE_EXTERNAL_SYSTEMS_INCOMPAT);
    }

    #[test]
    fn export_controls_is_constrained_when_either_list_is_non_empty() {
        assert!(!ExportControls::default().is_constrained());
        assert!(
            ExportControls {
                allowed_markings: vec!["public".into()],
                allowed_organizations: vec![],
            }
            .is_constrained()
        );
        assert!(
            ExportControls {
                allowed_markings: vec![],
                allowed_organizations: vec!["acme".into()],
            }
            .is_constrained()
        );
    }
}
