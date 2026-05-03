use axum::{Json, http::StatusCode, response::IntoResponse};

use crate::{
    domain::compiler,
    models::authoring::{CompilePipelineRequest, ValidatePipelineRequest},
};

pub async fn validate_pipeline(Json(body): Json<ValidatePipelineRequest>) -> impl IntoResponse {
    let validation = compiler::validate_request(&body);
    let status = if validation.valid {
        StatusCode::OK
    } else {
        StatusCode::BAD_REQUEST
    };
    (status, Json(validation)).into_response()
}

pub async fn compile_pipeline(Json(body): Json<CompilePipelineRequest>) -> impl IntoResponse {
    match compiler::compile_request(&body) {
        Ok(compiled) => Json(compiled).into_response(),
        Err(validation) => (StatusCode::BAD_REQUEST, Json(validation)).into_response(),
    }
}

pub async fn prune_pipeline(Json(body): Json<CompilePipelineRequest>) -> impl IntoResponse {
    match compiler::prune_request(&body) {
        Ok(pruned) => Json(pruned).into_response(),
        Err(validation) => (StatusCode::BAD_REQUEST, Json(validation)).into_response(),
    }
}
