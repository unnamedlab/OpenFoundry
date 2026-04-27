use axum::Json;

use crate::models::widget_type::WidgetCatalogItem;

pub async fn list_widget_catalog() -> Json<Vec<WidgetCatalogItem>> {
    Json(crate::models::widget_type::widget_catalog())
}
