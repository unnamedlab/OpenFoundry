//! S3.1.b / S3.1.c — Vault transit signing + JWKS rotation policy.
//!
//! In production the JWKS signing key never leaves Vault: callers
//! send the digest to the `transit/sign/<key>` endpoint and Vault
//! returns the signature. The public keys are mirrored into
//! `pg-schemas.auth_schema.jwks_keys` so `/.well-known/jwks.json`
//! can serve them without a Vault round-trip on every request.
//!
//! This module exposes a [`Signer`] trait and a [`VaultTransitSigner`]
//! stub that documents the production wiring. Tests exercise the
//! pure JWKS rotation calculator [`RotationPolicy`].

use async_trait::async_trait;
use chrono::{DateTime, Duration, Utc};

/// Signing-key identifier as held in Vault transit
/// (`transit/keys/<name>` + version).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VaultKeyRef {
    pub name: String,
    pub version: u32,
}

#[derive(Debug, thiserror::Error)]
pub enum SignError {
    #[error("vault transit not yet wired (S3.1.b follow-up)")]
    NotWired,
    #[error("vault returned non-200: {0}")]
    Upstream(String),
}

/// Pluggable signer abstraction. The HS256 fallback used during
/// dev / CI keeps `auth-middleware::jwt::encode_token` unchanged;
/// production identity-federation-service swaps in
/// [`VaultTransitSigner`].
#[async_trait]
pub trait Signer: Send + Sync {
    fn key_ref(&self) -> VaultKeyRef;
    async fn sign(&self, digest: &[u8]) -> Result<Vec<u8>, SignError>;
}

/// Production signer — logs + errors out. Real impl will dial Vault
/// `transit/sign/<key>` with the request bytes hashed by the
/// caller.
pub struct VaultTransitSigner {
    pub key: VaultKeyRef,
    pub vault_addr: String,
}

#[async_trait]
impl Signer for VaultTransitSigner {
    fn key_ref(&self) -> VaultKeyRef {
        self.key.clone()
    }

    async fn sign(&self, _digest: &[u8]) -> Result<Vec<u8>, SignError> {
        Err(SignError::NotWired)
    }
}

/// JWKS rotation policy — pure calendar arithmetic. ADR-0026 fixes
/// the cadence at 90 d active + 14 d grace, so two keys are
/// published in `/.well-known/jwks.json` between days 90 and 104 of
/// each cycle.
#[derive(Debug, Clone, Copy)]
pub struct RotationPolicy {
    pub active_days: i64,
    pub grace_days: i64,
}

impl RotationPolicy {
    pub const ASVS_L2_DEFAULT: Self = Self {
        active_days: 90,
        grace_days: 14,
    };

    /// Given the activation timestamp of the current key, return
    /// `(rotate_at, retire_at)`. `rotate_at` = when a new key must
    /// be published; `retire_at` = when the previous key is removed
    /// from JWKS.
    pub fn rotate_and_retire(&self, activated_at: DateTime<Utc>) -> (DateTime<Utc>, DateTime<Utc>) {
        let rotate_at = activated_at + Duration::days(self.active_days);
        let retire_at = rotate_at + Duration::days(self.grace_days);
        (rotate_at, retire_at)
    }

    /// Returns true iff `now` falls inside the dual-publication
    /// window where both the previous and the current key must
    /// appear in JWKS.
    pub fn is_in_grace(
        &self,
        prev_activated_at: DateTime<Utc>,
        now: DateTime<Utc>,
    ) -> bool {
        let (rotate_at, retire_at) = self.rotate_and_retire(prev_activated_at);
        now >= rotate_at && now < retire_at
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rotation_policy_default_is_90_14() {
        let p = RotationPolicy::ASVS_L2_DEFAULT;
        assert_eq!(p.active_days, 90);
        assert_eq!(p.grace_days, 14);
    }

    #[test]
    fn grace_window_includes_day_90_to_104() {
        let p = RotationPolicy::ASVS_L2_DEFAULT;
        let activated = Utc::now();
        let day_89 = activated + Duration::days(89);
        let day_91 = activated + Duration::days(91);
        let day_103 = activated + Duration::days(103);
        let day_105 = activated + Duration::days(105);

        assert!(!p.is_in_grace(activated, day_89));
        assert!(p.is_in_grace(activated, day_91));
        assert!(p.is_in_grace(activated, day_103));
        assert!(!p.is_in_grace(activated, day_105));
    }

    #[tokio::test]
    async fn vault_signer_stub_errors_without_wiring() {
        let s = VaultTransitSigner {
            key: VaultKeyRef {
                name: "of-jwks-active".into(),
                version: 1,
            },
            vault_addr: "http://vault:8200".into(),
        };
        assert!(matches!(s.sign(b"digest").await, Err(SignError::NotWired)));
    }
}
