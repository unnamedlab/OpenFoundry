//! Axum router builder for the B3 workspace surface (favorites,
//! recents, trash, sharing, resource ops). Mounted under
//! `/api/v1/workspace` by the service binary.

use axum::{
    Router,
    routing::{delete, get, post},
};

use crate::AppState;
use crate::handlers::{favorites, recents, resource_ops, sharing, trash};

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
        // Trash --------------------------------------------------------
        .route("/trash", get(trash::list_trash))
        .route(
            "/resources/{kind}/{id}/restore",
            post(trash::restore_resource),
        )
        .route(
            "/resources/{kind}/{id}/purge",
            delete(trash::purge_resource),
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
        // Resource operations -----------------------------------------
        .route(
            "/resources/{kind}/{id}/move",
            post(resource_ops::move_resource),
        )
        .route(
            "/resources/{kind}/{id}/rename",
            post(resource_ops::rename_resource),
        )
        .route(
            "/resources/{kind}/{id}/duplicate",
            post(resource_ops::duplicate_resource),
        )
        .route(
            "/resources/{kind}/{id}",
            delete(resource_ops::soft_delete_resource),
        )
        .route("/resources/batch", post(resource_ops::batch_apply))
}
