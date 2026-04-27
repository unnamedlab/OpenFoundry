pub mod common {
    use auth_middleware::Claims;
    use axum::{
        Json,
        http::StatusCode,
        response::{IntoResponse, Response},
    };
    use serde_json::json;

    pub fn json_error(status: StatusCode, message: impl Into<String>) -> Response {
        (status, Json(json!({ "error": message.into() }))).into_response()
    }

    pub fn require_permission(claims: &Claims, resource: &str, action: &str) -> Result<(), Response> {
        if claims.has_permission(resource, action) {
            Ok(())
        } else {
            Err(json_error(
                StatusCode::FORBIDDEN,
                format!("missing permission {resource}:{action}"),
            ))
        }
    }
}

pub mod login {
    use serde::Serialize;

    #[derive(Debug, Serialize)]
    #[serde(tag = "status", rename_all = "snake_case")]
    pub enum LoginResponse {
        Authenticated {
            access_token: String,
            refresh_token: String,
            token_type: String,
            expires_in: i64,
        },
        MfaRequired {
            challenge_token: String,
            methods: Vec<String>,
            expires_in: i64,
        },
    }
}

#[path = "../../identity-federation-service/src/handlers/api_key_mgmt.rs"]
pub mod api_key_mgmt;

#[path = "../../identity-federation-service/src/handlers/sso.rs"]
pub mod sso;
