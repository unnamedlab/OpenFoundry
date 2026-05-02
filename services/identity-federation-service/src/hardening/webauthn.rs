//! S3.1.d — WebAuthn second factor.
//!
//! The crate `webauthn-rs` carries the heavy crypto. This module
//! defines a small façade so handlers receive structured DTOs and
//! the relying-party config sits in one place. Real registration /
//! assertion endpoints land per the bin refactor.

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone)]
pub struct RelyingPartyConfig {
    pub rp_id: String,
    pub rp_origin: String,
    pub rp_name: String,
}

impl RelyingPartyConfig {
    pub fn from_env() -> Self {
        Self {
            rp_id: std::env::var("OF_WEBAUTHN_RP_ID")
                .unwrap_or_else(|_| "openfoundry.local".into()),
            rp_origin: std::env::var("OF_WEBAUTHN_RP_ORIGIN")
                .unwrap_or_else(|_| "https://openfoundry.local".into()),
            rp_name: std::env::var("OF_WEBAUTHN_RP_NAME").unwrap_or_else(|_| "OpenFoundry".into()),
        }
    }
}

/// What the browser sends in `navigator.credentials.create()` →
/// server.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RegisterCredentialRequest {
    pub user_id: String,
    pub display_name: String,
    /// Base64-url-encoded WebAuthn `attestationObject`.
    pub attestation: String,
    /// Base64-url-encoded `clientDataJSON`.
    pub client_data: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RegisterCredentialResponse {
    pub credential_id: String,
    pub aaguid: String,
}

#[derive(Debug, thiserror::Error)]
pub enum WebAuthnError {
    #[error("webauthn-rs adapter not yet wired (S3.1.d follow-up)")]
    NotWired,
}

/// Substrate stub. Production impl will hold a `webauthn_rs::Webauthn`
/// instance built from the relying-party config and validate the
/// attestation envelope.
pub struct WebAuthnAdapter {
    pub config: RelyingPartyConfig,
}

impl WebAuthnAdapter {
    pub fn new(config: RelyingPartyConfig) -> Self {
        Self { config }
    }

    pub fn register(
        &self,
        _req: &RegisterCredentialRequest,
    ) -> Result<RegisterCredentialResponse, WebAuthnError> {
        Err(WebAuthnError::NotWired)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn register_returns_not_wired_until_handler_lands() {
        let a = WebAuthnAdapter::new(RelyingPartyConfig {
            rp_id: "openfoundry.local".into(),
            rp_origin: "https://openfoundry.local".into(),
            rp_name: "OpenFoundry".into(),
        });
        let req = RegisterCredentialRequest {
            user_id: "u1".into(),
            display_name: "User".into(),
            attestation: "AA".into(),
            client_data: "BB".into(),
        };
        assert!(matches!(a.register(&req), Err(WebAuthnError::NotWired)));
    }
}
