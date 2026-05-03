//! Axum router builder for the tenancy-owned subset of the B3 workspace
//! surface (favorites, recents, sharing). Mounted under
//! `/api/v1/workspace` by the service binary.
//!
//! Legacy cross-BC resource-management routes were direct-SQL shims into
//! `ontology` / `nexus`; they are intentionally no longer exposed from
//! tenancy until upstream APIs or local read-models exist.

use axum::{
    Router,
    routing::{delete, get, post},
};

use crate::AppState;
use crate::handlers::{favorites, recents, sharing};

pub fn workspace_router() -> Router<AppState> {
    Router::new()
        // Favorites ----------------------------------------------------
        .route(
            "/favorites",
            post(favorites::create_favorite).get(favorites::list_favorites),
        )
        .route(
            "/favorites/{kind}/{id}",
            delete(favorites::delete_favorite),
        )
        // Recents ------------------------------------------------------
        .route(
            "/recents",
            post(recents::record_access).get(recents::list_recents),
        )
        // Sharing ------------------------------------------------------
        .route(
            "/resources/{kind}/{id}/share",
            post(sharing::create_share),
        )
        .route(
            "/resources/{kind}/{id}/shares",
            get(sharing::list_resource_shares),
        )
        .route("/shares/{id}", delete(sharing::revoke_share))
        .route("/shared-with-me", get(sharing::list_shared_with_me))
        .route("/shared-by-me", get(sharing::list_shared_by_me))
}
