use axum::Json;

use crate::{
    domain::sds,
    handlers::{ServiceResult, bad_request},
    models::sensitive_data::{SensitiveDataScanRequest, SensitiveDataScanResponse},
};

pub async fn scan_sensitive_data(
    Json(request): Json<SensitiveDataScanRequest>,
) -> ServiceResult<SensitiveDataScanResponse> {
    if request.content.trim().is_empty() {
        return Err(bad_request("content is required"));
    }
    Ok(Json(sds::scan(&request)))
}
