pub mod app;
pub mod binding;
pub mod event;
pub mod page;
pub mod theme;
pub mod version;
pub mod widget;
pub mod widget_type;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

/// Legacy composition_views placeholder model retained for schema continuity.
#[derive(Debug, Clone, Serialize, FromRow)]
pub struct CompositionView {
    pub id: Uuid,
    pub payload: serde_json::Value,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateCompositionViewRequest {
    pub payload: serde_json::Value,
}

/// Legacy composition_bindings placeholder model retained for schema continuity.
#[derive(Debug, Clone, Serialize, FromRow)]
pub struct CompositionBinding {
    pub id: Uuid,
    pub parent_id: Uuid,
    pub payload: serde_json::Value,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateCompositionBindingRequest {
    pub payload: serde_json::Value,
}
