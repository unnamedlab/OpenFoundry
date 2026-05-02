//! S3.1.c — JWKS publication (active + grace key set).
//!
//! Pure calculation: given the active and previous key public
//! material plus their activation timestamps, return the JWKS that
//! must be served at `/.well-known/jwks.json`. The handler that
//! mounts this is in the bin (S3.1.c follow-up); during the cutover
//! it lives here so it can be unit-tested without an HTTP layer.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::vault_signer::RotationPolicy;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PublicKeyEntry {
    pub kid: String,
    pub kty: String,
    /// PEM-encoded public key, mirrored from `pg-schemas.auth_schema.jwks_keys`.
    pub public_pem: String,
    pub activated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Jwks {
    pub keys: Vec<JwkEntry>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct JwkEntry {
    pub kid: String,
    pub kty: String,
    pub public_pem: String,
    /// `"sig"` — these keys are publication-only; signing is done by Vault transit.
    #[serde(rename = "use")]
    pub use_: String,
    /// `"active"` or `"grace"` — non-standard, useful in audit/logs.
    pub status: String,
}

pub fn build_jwks(
    active: &PublicKeyEntry,
    previous: Option<&PublicKeyEntry>,
    policy: RotationPolicy,
    now: DateTime<Utc>,
) -> Jwks {
    let mut keys = vec![JwkEntry {
        kid: active.kid.clone(),
        kty: active.kty.clone(),
        public_pem: active.public_pem.clone(),
        use_: "sig".into(),
        status: "active".into(),
    }];
    if let Some(prev) = previous {
        if policy.is_in_grace(prev.activated_at, now) {
            keys.push(JwkEntry {
                kid: prev.kid.clone(),
                kty: prev.kty.clone(),
                public_pem: prev.public_pem.clone(),
                use_: "sig".into(),
                status: "grace".into(),
            });
        }
    }
    Jwks { keys }
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::Duration;

    fn key(kid: &str, days_ago: i64) -> PublicKeyEntry {
        PublicKeyEntry {
            kid: kid.into(),
            kty: "EC".into(),
            public_pem: format!("-----BEGIN PUBLIC KEY-----\n{kid}\n-----END PUBLIC KEY-----"),
            activated_at: Utc::now() - Duration::days(days_ago),
        }
    }

    #[test]
    fn single_key_outside_grace() {
        let active = key("k2", 5);
        let previous = key("k1", 200);
        let jwks = build_jwks(
            &active,
            Some(&previous),
            RotationPolicy::ASVS_L2_DEFAULT,
            Utc::now(),
        );
        assert_eq!(jwks.keys.len(), 1);
        assert_eq!(jwks.keys[0].kid, "k2");
    }

    #[test]
    fn two_keys_inside_grace() {
        let active = key("k2", 5);
        let previous = key("k1", 95);
        let jwks = build_jwks(
            &active,
            Some(&previous),
            RotationPolicy::ASVS_L2_DEFAULT,
            Utc::now(),
        );
        assert_eq!(jwks.keys.len(), 2);
        assert_eq!(jwks.keys[1].status, "grace");
    }
}
