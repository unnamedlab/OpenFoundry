//! `GET /iceberg/v1/config` — REST Catalog client bootstrap. Returns
//! the warehouse identifier so PyIceberg / Spark know which Foundry
//! warehouse the catalog routes to.

use axum::{Json, extract::State};
use serde::Serialize;
use std::collections::HashMap;

use crate::AppState;
use crate::handlers::auth::bearer::AuthenticatedPrincipal;
use crate::handlers::errors::ApiError;
use crate::metrics;

#[derive(Debug, Serialize)]
pub struct ConfigResponse {
    pub defaults: HashMap<String, String>,
    pub overrides: HashMap<String, String>,
}

pub async fn get_config(
    State(state): State<AppState>,
    _principal: AuthenticatedPrincipal,
) -> Result<Json<ConfigResponse>, ApiError> {
    let mut defaults = HashMap::new();
    defaults.insert("warehouse".to_string(), state.iceberg.warehouse_uri.clone());
    metrics::record_rest_request("GET", "/iceberg/v1/config", 200);
    Ok(Json(ConfigResponse {
        defaults,
        overrides: HashMap::new(),
    }))
}
