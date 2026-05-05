//! Markings handlers — namespace + table.
//!
//! Wraps `domain::markings` with HTTP semantics + Cedar enforcement.
//!
//!   * `GET .../namespaces/{ns}/markings` → namespace markings
//!     projection.
//!   * `POST .../namespaces/{ns}/markings` → replace explicit markings
//!     on the namespace. Requires
//!     `iceberg::namespace::manage_markings`.
//!   * `GET .../namespaces/{ns}/tables/{tbl}/markings` → effective +
//!     explicit + inherited triple.
//!   * `PATCH .../namespaces/{ns}/tables/{tbl}/markings` → replace
//!     explicit markings. Requires `iceberg::table::manage_markings`.

use axum::extract::{Path, State};
use axum::http::HeaderMap;
use axum::Json;
use serde::Deserialize;
use uuid::Uuid;

use crate::AppState;
use crate::audit;
use crate::authz::{self, AuthzResource, NamespaceAttrs, PrincipalKind, TableAttrs};
use crate::domain::markings::{self, NamespaceMarkings, TableMarkings};
use crate::domain::namespace::{self, decode_path};
use crate::domain::table;
use crate::handlers::auth::bearer::AuthenticatedPrincipal;
use crate::handlers::errors::ApiError;
use crate::handlers::rest_catalog::resolve_project_rid;

/// Body for `POST .../namespaces/{ns}/markings` and `PATCH
/// .../namespaces/{ns}/tables/{tbl}/markings`.
#[derive(Debug, Deserialize)]
pub struct UpdateMarkingsRequest {
    /// Replacement set of marking names. The server resolves each
    /// name to a `marking_id` via `iceberg_marking_names`; unknown
    /// names return 400.
    pub markings: Vec<String>,
}

pub async fn get_namespace_markings(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path(namespace_path): Path<String>,
    principal: AuthenticatedPrincipal,
) -> Result<Json<NamespaceMarkings>, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let ns = namespace::fetch(&state.iceberg.db, &project_rid, &decode_path(&namespace_path)).await?;
    let projection = markings::for_namespace(&state.iceberg.db, &ns)
        .await
        .map_err(|err| ApiError::Internal(err.to_string()))?;

    let resource = AuthzResource::Namespace(NamespaceAttrs::from_namespace(
        &ns,
        marking_names(&projection.effective),
        &state.iceberg.default_tenant,
    ));
    authz::enforce(
        state.iceberg.authz.as_ref(),
        &principal,
        principal_kind(&principal),
        "iceberg::namespace::view",
        &resource,
        &state.iceberg.default_tenant,
    )
    .await?;

    Ok(Json(projection))
}

pub async fn update_namespace_markings(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path(namespace_path): Path<String>,
    principal: AuthenticatedPrincipal,
    Json(body): Json<UpdateMarkingsRequest>,
) -> Result<Json<NamespaceMarkings>, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let ns =
        namespace::fetch(&state.iceberg.db, &project_rid, &decode_path(&namespace_path)).await?;

    // Authorization: manage_markings on the namespace.
    let before = markings::for_namespace(&state.iceberg.db, &ns)
        .await
        .map_err(|err| ApiError::Internal(err.to_string()))?;
    let resource = AuthzResource::Namespace(NamespaceAttrs::from_namespace(
        &ns,
        marking_names(&before.effective),
        &state.iceberg.default_tenant,
    ));
    authz::enforce(
        state.iceberg.authz.as_ref(),
        &principal,
        principal_kind(&principal),
        "iceberg::namespace::manage_markings",
        &resource,
        &state.iceberg.default_tenant,
    )
    .await?;

    let actor = parse_actor(&principal);
    let ids = resolve_marking_ids(&state, &body.markings).await?;
    let projection = markings::set_namespace_markings(&state.iceberg.db, &ns, &ids, actor)
        .await
        .map_err(|err| ApiError::Internal(err.to_string()))?;

    audit::markings_updated(
        actor,
        &format!("ri.foundry.main.iceberg-namespace.{}", ns.id),
        "namespace",
        &marking_names(&before.effective),
        &marking_names(&projection.effective),
    );

    Ok(Json(projection))
}

pub async fn get_table_markings(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path((namespace_path, table_name)): Path<(String, String)>,
    principal: AuthenticatedPrincipal,
) -> Result<Json<TableMarkings>, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let ns =
        namespace::fetch(&state.iceberg.db, &project_rid, &decode_path(&namespace_path)).await?;
    let tab = table::fetch(&state.iceberg.db, &ns, &table_name).await?;
    let projection = markings::for_table(&state.iceberg.db, &tab)
        .await
        .map_err(|err| ApiError::Internal(err.to_string()))?;

    let resource = AuthzResource::Table(TableAttrs::from_table(
        &tab,
        marking_names(&projection.effective),
        marking_names(&projection.explicit),
        &state.iceberg.default_tenant,
    ));
    authz::enforce(
        state.iceberg.authz.as_ref(),
        &principal,
        principal_kind(&principal),
        "iceberg::table::view",
        &resource,
        &state.iceberg.default_tenant,
    )
    .await?;

    Ok(Json(projection))
}

pub async fn update_table_markings(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path((namespace_path, table_name)): Path<(String, String)>,
    principal: AuthenticatedPrincipal,
    Json(body): Json<UpdateMarkingsRequest>,
) -> Result<Json<TableMarkings>, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let ns =
        namespace::fetch(&state.iceberg.db, &project_rid, &decode_path(&namespace_path)).await?;
    let tab = table::fetch(&state.iceberg.db, &ns, &table_name).await?;

    let before = markings::for_table(&state.iceberg.db, &tab)
        .await
        .map_err(|err| ApiError::Internal(err.to_string()))?;
    let resource = AuthzResource::Table(TableAttrs::from_table(
        &tab,
        marking_names(&before.effective),
        marking_names(&before.explicit),
        &state.iceberg.default_tenant,
    ));
    authz::enforce(
        state.iceberg.authz.as_ref(),
        &principal,
        principal_kind(&principal),
        "iceberg::table::manage_markings",
        &resource,
        &state.iceberg.default_tenant,
    )
    .await?;

    let actor = parse_actor(&principal);
    let ids = resolve_marking_ids(&state, &body.markings).await?;
    let projection = markings::set_table_explicit_markings(&state.iceberg.db, &tab, &ids, actor)
        .await
        .map_err(|err| ApiError::Internal(err.to_string()))?;

    for new_name in marking_names(&projection.explicit)
        .into_iter()
        .filter(|n| !marking_names(&before.explicit).contains(n))
    {
        audit::markings_override_created(actor, &tab.rid, &new_name);
    }
    audit::markings_updated(
        actor,
        &tab.rid,
        "table",
        &marking_names(&before.effective),
        &marking_names(&projection.effective),
    );

    Ok(Json(projection))
}

fn principal_kind(principal: &AuthenticatedPrincipal) -> PrincipalKind {
    if principal.scopes.iter().any(|s| s.starts_with("svc:")) {
        PrincipalKind::ServicePrincipal
    } else {
        PrincipalKind::User
    }
}

fn parse_actor(principal: &AuthenticatedPrincipal) -> Uuid {
    Uuid::parse_str(&principal.subject).unwrap_or_else(|_| Uuid::nil())
}

fn marking_names(items: &[markings::MarkingProjection]) -> Vec<String> {
    items.iter().map(|p| p.name.clone()).collect()
}

async fn resolve_marking_ids(
    state: &AppState,
    names: &[String],
) -> Result<Vec<Uuid>, ApiError> {
    let mut ids = Vec::with_capacity(names.len());
    for name in names {
        let id = markings::resolve_name(&state.iceberg.db, name)
            .await
            .map_err(|err| ApiError::BadRequest(err.to_string()))?;
        ids.push(id.as_uuid());
    }
    Ok(ids)
}
