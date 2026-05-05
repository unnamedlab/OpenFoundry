//! REST Catalog spec handlers (Apache Iceberg `rest-catalog-open-api.yaml`).
//!
//! Each submodule corresponds to a section of the spec:
//!
//! * [`config`]       — `/iceberg/v1/config`
//! * [`namespaces`]   — `/iceberg/v1/namespaces*`
//! * [`tables`]       — `/iceberg/v1/namespaces/{ns}/tables*`
//! * [`transactions`] — `/iceberg/v1/transactions/commit`

pub mod config;
pub mod namespaces;
pub mod tables;
pub mod transactions;

/// Resolve the Foundry `project_rid` for the calling request.
///
/// External Iceberg clients don't carry a project header — when the
/// authentication path resolves a Foundry user we pin the project
/// to the user's primary project. For the Beta we accept an explicit
/// `X-Foundry-Project-Rid` header so the UI can scope namespaces.
pub fn resolve_project_rid(headers: &axum::http::HeaderMap) -> String {
    headers
        .get("x-foundry-project-rid")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("ri.foundry.main.project.default")
        .to_string()
}
