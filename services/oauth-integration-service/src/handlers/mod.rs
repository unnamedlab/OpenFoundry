pub mod api_key_mgmt;
pub mod applications;
pub mod common;
pub mod external_integrations;
pub mod oauth_clients;
pub mod sso;

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
