use axum::{Json, extract::State, http::StatusCode, response::IntoResponse};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::AppState;

#[derive(Debug, Deserialize)]
pub struct RegisterRequest {
    pub email: String,
    pub password: String,
    pub name: String,
}

#[derive(Debug, Serialize)]
pub struct RegisterResponse {
    pub id: Uuid,
    pub email: String,
    pub name: String,
}

#[derive(Debug, Serialize)]
pub struct BootstrapStatusResponse {
    pub requires_initial_admin: bool,
}

pub async fn bootstrap_status(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM users")
        .fetch_one(&state.db)
        .await
    {
        Ok(user_count) => Json(BootstrapStatusResponse {
            requires_initial_admin: user_count == 0,
        })
        .into_response(),
        Err(e) => {
            tracing::error!("failed to load bootstrap status: {e}");
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": "failed to load bootstrap status" })),
            )
                .into_response()
        }
    }
}

pub async fn register(
    State(state): State<AppState>,
    Json(body): Json<RegisterRequest>,
) -> impl IntoResponse {
    let password_hash = match hash_password(&body.password) {
        Ok(h) => h,
        Err(_) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": "failed to hash password" })),
            )
                .into_response();
        }
    };

    let mut tx = match state.db.begin().await {
        Ok(tx) => tx,
        Err(e) => {
            tracing::error!("failed to start registration transaction: {e}");
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": "registration failed" })),
            )
                .into_response();
        }
    };

    if let Err(e) = sqlx::query("SELECT pg_advisory_xact_lock($1)")
        .bind(8_514_200_001_i64)
        .execute(&mut *tx)
        .await
    {
        tracing::error!("failed to acquire registration bootstrap lock: {e}");
        return (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({ "error": "registration failed" })),
        )
            .into_response();
    }

    match sqlx::query_scalar::<_, bool>("SELECT EXISTS(SELECT 1 FROM users WHERE email = $1)")
        .bind(&body.email)
        .fetch_one(&mut *tx)
        .await
    {
        Ok(true) => {
            return (
                StatusCode::CONFLICT,
                Json(serde_json::json!({ "error": "email already registered" })),
            )
                .into_response();
        }
        Ok(false) => {}
        Err(e) => {
            tracing::error!("failed to check existing user during registration: {e}");
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": "registration failed" })),
            )
                .into_response();
        }
    }

    let existing_user_count = match sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM users")
        .fetch_one(&mut *tx)
        .await
    {
        Ok(count) => count,
        Err(e) => {
            tracing::error!("failed to count existing users during registration: {e}");
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": "registration failed" })),
            )
                .into_response();
        }
    };

    let user_id = Uuid::now_v7();
    let result = sqlx::query(
        r#"INSERT INTO users (id, email, name, password_hash, is_active, auth_source)
              VALUES ($1, $2, $3, $4, true, 'local')"#,
    )
    .bind(user_id)
    .bind(&body.email)
    .bind(&body.name)
    .bind(&password_hash)
    .execute(&mut *tx)
    .await;

    match result {
        Ok(_) => {
            let role_name = if existing_user_count == 0 {
                "admin"
            } else {
                "viewer"
            };
            let role_id =
                match sqlx::query_scalar::<_, Uuid>("SELECT id FROM roles WHERE name = $1")
                    .bind(role_name)
                    .fetch_optional(&mut *tx)
                    .await
                {
                    Ok(Some(role_id)) => role_id,
                    Ok(None) => {
                        tracing::error!("registration role {role_name} does not exist");
                        return (
                            StatusCode::INTERNAL_SERVER_ERROR,
                            Json(serde_json::json!({ "error": "registration failed" })),
                        )
                            .into_response();
                    }
                    Err(e) => {
                        tracing::error!("failed to load role {role_name} during registration: {e}");
                        return (
                            StatusCode::INTERNAL_SERVER_ERROR,
                            Json(serde_json::json!({ "error": "registration failed" })),
                        )
                            .into_response();
                    }
                };

            let role_assigned = match sqlx::query(
                r#"INSERT INTO user_roles (user_id, role_id)
                   VALUES ($1, $2)
                   ON CONFLICT DO NOTHING"#,
            )
            .bind(user_id)
            .bind(role_id)
            .execute(&mut *tx)
            .await
            {
                Ok(result) => result,
                Err(e) => {
                    tracing::error!("failed to assign role during registration: {e}");
                    return (
                        StatusCode::INTERNAL_SERVER_ERROR,
                        Json(serde_json::json!({ "error": "registration failed" })),
                    )
                        .into_response();
                }
            };

            if role_assigned.rows_affected() == 0 {
                tracing::error!("registration completed without assigning role {role_name}");
                return (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    Json(serde_json::json!({ "error": "registration failed" })),
                )
                    .into_response();
            }

            if let Err(e) = tx.commit().await {
                tracing::error!("failed to commit registration transaction: {e}");
                return (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    Json(serde_json::json!({ "error": "registration failed" })),
                )
                    .into_response();
            }

            tracing::info!(
                user_id = %user_id,
                email = %body.email,
                assigned_role = role_name,
                "user registered"
            );

            (
                StatusCode::CREATED,
                Json(serde_json::json!(RegisterResponse {
                    id: user_id,
                    email: body.email,
                    name: body.name,
                })),
            )
                .into_response()
        }
        Err(e) => {
            tracing::error!("registration failed: {e}");
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": "registration failed" })),
            )
                .into_response()
        }
    }
}

fn hash_password(password: &str) -> Result<String, argon2::password_hash::Error> {
    use argon2::password_hash::SaltString;
    use argon2::password_hash::rand_core::OsRng;
    use argon2::{Argon2, PasswordHasher};

    let salt = SaltString::generate(&mut OsRng);
    let argon2 = Argon2::default();
    let hash = argon2.hash_password(password.as_bytes(), &salt)?;
    Ok(hash.to_string())
}
