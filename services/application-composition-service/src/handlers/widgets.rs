// Widget catalog + host bridge endpoints. Consolidated from the
// retired `widget-registry-service` (S8.1.b, ADR-0030).
//
// The single read-only catalog endpoint stays unchanged; future
// CRUD over widget instances will land here as it is added.

use axum::Json;

use crate::models::widgets::WidgetCatalogItem;

pub async fn list_widget_catalog() -> Json<Vec<WidgetCatalogItem>> {
    Json(crate::models::widgets::widget_catalog())
}
