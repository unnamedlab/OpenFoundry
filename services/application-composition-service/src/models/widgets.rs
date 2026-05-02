// Widget catalog model. Consolidated from the retired
// `widget-registry-service` (S8.1.b, ADR-0030).
//
// The implementation still lives in `app-builder-service` until
// that service is retired in its own R-prompt. The
// `#[path = ...]` re-export keeps the source of truth in one place
// without duplicating the catalog data.

#[path = "../../../app-builder-service/src/models/widget_type.rs"]
pub mod widget_type;

pub use widget_type::{WidgetCatalogItem, WidgetDefaultSize};

/// Returns the static widget catalog. Backed by
/// `app-builder-service`'s `widget_type::widget_catalog()` until the
/// catalog is migrated into this crate as part of the
/// `app-builder-service` retirement PR.
pub fn widget_catalog() -> Vec<WidgetCatalogItem> {
    widget_type::widget_catalog()
}
