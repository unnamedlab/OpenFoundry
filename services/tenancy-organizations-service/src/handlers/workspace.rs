//! Shared primitives for the workspace handlers (B3 Phase 1).
//!
//! All workspace handlers rely on a single canonical `ResourceKind`
//! enum, parsed from URL path segments and request bodies, plus a small
//! handful of error helpers that emit consistent JSON envelopes.

use axum::{
    Json,
    http::StatusCode,
    response::{IntoResponse, Response},
};
use serde_json::json;

/// Canonical set of resource kinds the workspace surface knows about.
///
/// Adding a new kind requires:
/// 1. Adding a variant here;
/// 2. Updating [`ResourceKind::parse`] / [`ResourceKind::as_str`];
/// 3. Wiring trash/sharing/move handlers for the new kind.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ResourceKind {
    // Ontology workspace (owned by ontology-definition-service).
    OntologyProject,
    OntologyFolder,
    OntologyResourceBinding,
    // Other workspace surfaces. Sharing / favorites / recents accept
    // these but trash/move are delegated to the resource-owning service.
    Dataset,
    Pipeline,
    Notebook,
    App,
    Dashboard,
    Report,
    Model,
    Workflow,
    Other,
}

impl ResourceKind {
    pub fn parse(value: &str) -> Result<Self, String> {
        Ok(match value.trim() {
            "ontology_project" | "project" => Self::OntologyProject,
            "ontology_folder" | "folder" => Self::OntologyFolder,
            "ontology_resource_binding" | "resource_binding" => Self::OntologyResourceBinding,
            "dataset" => Self::Dataset,
            "pipeline" => Self::Pipeline,
            "notebook" => Self::Notebook,
            "app" => Self::App,
            "dashboard" => Self::Dashboard,
            "report" => Self::Report,
            "model" => Self::Model,
            "workflow" => Self::Workflow,
            "other" => Self::Other,
            other => {
                return Err(format!(
                    "resource_kind '{other}' is not supported; expected one of: \
                     ontology_project, ontology_folder, ontology_resource_binding, \
                     dataset, pipeline, notebook, app, dashboard, report, model, \
                     workflow, other"
                ));
            }
        })
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Self::OntologyProject => "ontology_project",
            Self::OntologyFolder => "ontology_folder",
            Self::OntologyResourceBinding => "ontology_resource_binding",
            Self::Dataset => "dataset",
            Self::Pipeline => "pipeline",
            Self::Notebook => "notebook",
            Self::App => "app",
            Self::Dashboard => "dashboard",
            Self::Report => "report",
            Self::Model => "model",
            Self::Workflow => "workflow",
            Self::Other => "other",
        }
    }
}

pub fn bad(message: impl Into<String>) -> Response {
    (
        StatusCode::BAD_REQUEST,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

pub fn forbidden(message: impl Into<String>) -> Response {
    (
        StatusCode::FORBIDDEN,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

pub fn db_err(context: &'static str, error: sqlx::Error) -> Response {
    tracing::error!(target: "workspace", "{context}: {error}");
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": format!("{context}: {error}") })),
    )
        .into_response()
}
