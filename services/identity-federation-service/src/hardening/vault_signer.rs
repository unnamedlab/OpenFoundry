//! S3.1.b / S3.1.c — Vault transit signing + JWKS rotation policy.
//!
//! In production the JWT/JWKS signing key never leaves Vault: callers
//! hash the JOSE signing input, send that digest to
//! `transit/sign/<key>` with `prehashed=true`, and Vault returns the
//! signature bytes. The public keys are mirrored into
//! `pg-schemas.auth_schema.jwks_keys` so `/.well-known/jwks.json`
//! can serve them without a Vault round-trip on every request.

use std::env;
use std::time::Duration;

use async_trait::async_trait;
use base64::Engine;
use base64::engine::general_purpose::{STANDARD as B64, URL_SAFE_NO_PAD};
use chrono::{DateTime, Duration as ChronoDuration, Utc};
use reqwest::{StatusCode, Url};
use serde::{Deserialize, Serialize};

const DEFAULT_VAULT_TIMEOUT_MS: u64 = 2_000;
const DEFAULT_RETRY_ATTEMPTS: usize = 3;
const DEFAULT_RETRY_BACKOFF_MS: u64 = 100;
const DEFAULT_TRANSIT_MOUNT: &str = "transit";
const DEFAULT_AUTH_MOUNT: &str = "kubernetes";
const DEFAULT_K8S_JWT_PATH: &str = "/var/run/secrets/kubernetes.io/serviceaccount/token";
const DEFAULT_HASH_ALGORITHM: &str = "sha2-256";
const DEFAULT_SIGNATURE_ALGORITHM: &str = "pkcs1v15";

/// Signing-key identifier as held in Vault transit
/// (`transit/keys/<name>` + version).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct VaultKeyRef {
    pub name: String,
    pub version: u32,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VaultRetryPolicy {
    pub attempts: usize,
    pub backoff: Duration,
}

impl Default for VaultRetryPolicy {
    fn default() -> Self {
        Self {
            attempts: DEFAULT_RETRY_ATTEMPTS,
            backoff: Duration::from_millis(DEFAULT_RETRY_BACKOFF_MS),
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum VaultAuthConfig {
    Token(String),
    KubernetesRole {
        role: String,
        mount: String,
        jwt_path: String,
    },
}

#[derive(Debug, Clone)]
pub struct VaultTransitConfig {
    pub vault_addr: String,
    pub auth: VaultAuthConfig,
    pub key: VaultKeyRef,
    pub transit_mount: String,
    pub timeout: Duration,
    pub retry_policy: VaultRetryPolicy,
    pub hash_algorithm: String,
    pub signature_algorithm: String,
}

impl VaultTransitConfig {
    pub fn from_env() -> Result<Self, SignError> {
        let vault_addr = required_env("VAULT_ADDR")?;
        let key = VaultKeyRef {
            name: first_env(&[
                "VAULT_TRANSIT_KEY",
                "VAULT_TRANSIT_KEY_NAME",
                "OPENFOUNDRY_JWT_TRANSIT_KEY",
            ])
            .ok_or_else(|| {
                SignError::Config(
                    "missing VAULT_TRANSIT_KEY/VAULT_TRANSIT_KEY_NAME/OPENFOUNDRY_JWT_TRANSIT_KEY"
                        .to_string(),
                )
            })?,
            version: optional_env_u32("VAULT_TRANSIT_KEY_VERSION")?.unwrap_or(1),
        };

        if key.version == 0 {
            return Err(SignError::Config(
                "VAULT_TRANSIT_KEY_VERSION must be greater than zero".to_string(),
            ));
        }

        let auth = if let Some(token) = non_empty_env("VAULT_TOKEN") {
            VaultAuthConfig::Token(token)
        } else if let Some(role) = non_empty_env("VAULT_ROLE") {
            VaultAuthConfig::KubernetesRole {
                role,
                mount: non_empty_env("VAULT_AUTH_MOUNT")
                    .unwrap_or_else(|| DEFAULT_AUTH_MOUNT.to_string()),
                jwt_path: non_empty_env("VAULT_K8S_JWT_PATH")
                    .unwrap_or_else(|| DEFAULT_K8S_JWT_PATH.to_string()),
            }
        } else {
            return Err(SignError::Config(
                "missing VAULT_TOKEN or VAULT_ROLE for Vault authentication".to_string(),
            ));
        };

        Ok(Self {
            vault_addr,
            auth,
            key,
            transit_mount: non_empty_env("VAULT_TRANSIT_MOUNT")
                .unwrap_or_else(|| DEFAULT_TRANSIT_MOUNT.to_string()),
            timeout: Duration::from_millis(
                optional_env_u64("VAULT_TIMEOUT_MS")?.unwrap_or(DEFAULT_VAULT_TIMEOUT_MS),
            ),
            retry_policy: VaultRetryPolicy {
                attempts: optional_env_usize("VAULT_RETRY_ATTEMPTS")?
                    .unwrap_or(DEFAULT_RETRY_ATTEMPTS)
                    .max(1),
                backoff: Duration::from_millis(
                    optional_env_u64("VAULT_RETRY_BACKOFF_MS")?.unwrap_or(DEFAULT_RETRY_BACKOFF_MS),
                ),
            },
            hash_algorithm: non_empty_env("VAULT_TRANSIT_HASH_ALGORITHM")
                .unwrap_or_else(|| DEFAULT_HASH_ALGORITHM.to_string()),
            signature_algorithm: non_empty_env("VAULT_TRANSIT_SIGNATURE_ALGORITHM")
                .unwrap_or_else(|| DEFAULT_SIGNATURE_ALGORITHM.to_string()),
        })
    }
}

#[derive(Debug, thiserror::Error)]
pub enum SignError {
    #[error("vault transit configuration error: {0}")]
    Config(String),
    #[error("vault auth failed: {0}")]
    Auth(String),
    #[error("vault http error during {context}: {source}")]
    Http {
        context: &'static str,
        #[source]
        source: reqwest::Error,
    },
    #[error("vault returned {status} during {context}: {body}")]
    Upstream {
        context: &'static str,
        status: StatusCode,
        body: String,
    },
    #[error("vault response missing field: {0}")]
    InvalidResponse(&'static str),
    #[error("vault transit signature has invalid format: {0}")]
    InvalidSignature(String),
}

impl SignError {
    fn retryable(&self) -> bool {
        match self {
            SignError::Http { source, .. } => source.is_timeout() || source.is_connect(),
            SignError::Upstream { status, .. } => {
                *status == StatusCode::TOO_MANY_REQUESTS || status.is_server_error()
            }
            _ => false,
        }
    }
}

/// Pluggable signer abstraction. Callers pass the SHA-256 digest of the
/// JWT/JWKS signing input and receive the signature bytes returned by
/// Vault Transit.
#[async_trait]
pub trait Signer: Send + Sync {
    fn key_ref(&self) -> VaultKeyRef;
    async fn sign(&self, digest: &[u8]) -> Result<Vec<u8>, SignError>;
}

/// Production signer backed by Vault Transit.
pub struct VaultTransitSigner {
    config: VaultTransitConfig,
    http: reqwest::Client,
    role_token_cache: tokio::sync::RwLock<Option<String>>,
}

impl VaultTransitSigner {
    pub fn new(config: VaultTransitConfig) -> Result<Self, SignError> {
        let http = reqwest::Client::builder()
            .timeout(config.timeout)
            .build()
            .map_err(|source| SignError::Http {
                context: "build_client",
                source,
            })?;
        Ok(Self {
            config,
            http,
            role_token_cache: tokio::sync::RwLock::new(None),
        })
    }

    pub fn from_env() -> Result<Self, SignError> {
        Self::new(VaultTransitConfig::from_env()?)
    }

    pub fn config(&self) -> &VaultTransitConfig {
        &self.config
    }

    /// Sign with an explicit transit key version. JWKS rotation can use
    /// this to issue with the newly active key while the previous public
    /// key remains in the grace window.
    pub async fn sign_with_key(
        &self,
        key: &VaultKeyRef,
        digest: &[u8],
    ) -> Result<Vec<u8>, SignError> {
        let attempts = self.config.retry_policy.attempts.max(1);
        for attempt in 1..=attempts {
            match self.sign_once(key, digest).await {
                Ok(signature) => return Ok(signature),
                Err(error) if attempt < attempts && error.retryable() => {
                    tracing::warn!(
                        attempt,
                        attempts,
                        error = %error,
                        "vault transit signing failed; retrying"
                    );
                    tokio::time::sleep(self.config.retry_policy.backoff * attempt as u32).await;
                }
                Err(error) => return Err(error),
            }
        }
        unreachable!("retry loop always returns");
    }

    pub async fn rotate_key(&self) -> Result<VaultKeyRef, SignError> {
        let token = self.vault_token().await?;
        let response = self
            .http
            .post(self.url(&[
                self.config.transit_mount.as_str(),
                "keys",
                self.config.key.name.as_str(),
                "rotate",
            ])?)
            .header("X-Vault-Token", token)
            .send()
            .await
            .map_err(|source| SignError::Http {
                context: "transit_rotate",
                source,
            })?;

        let status = response.status();
        let body = response.text().await.map_err(|source| SignError::Http {
            context: "read_transit_rotate",
            source,
        })?;
        if !status.is_success() {
            return Err(SignError::Upstream {
                context: "transit_rotate",
                status,
                body,
            });
        }
        self.latest_key_ref().await
    }

    pub async fn latest_key_ref(&self) -> Result<VaultKeyRef, SignError> {
        let metadata = self.key_metadata().await?;
        Ok(VaultKeyRef {
            name: self.config.key.name.clone(),
            version: metadata.data.latest_version,
        })
    }

    pub async fn public_key_pem(&self, key: &VaultKeyRef) -> Result<String, SignError> {
        let metadata = self.key_metadata().await?;
        let version = key.version.to_string();
        metadata
            .data
            .keys
            .get(&version)
            .and_then(|entry| entry.public_key.clone())
            .ok_or(SignError::InvalidResponse("data.keys[version].public_key"))
    }

    async fn sign_once(&self, key: &VaultKeyRef, digest: &[u8]) -> Result<Vec<u8>, SignError> {
        let token = self.vault_token().await?;
        let request = VaultSignRequest {
            input: B64.encode(digest),
            prehashed: true,
            hash_algorithm: &self.config.hash_algorithm,
            signature_algorithm: &self.config.signature_algorithm,
            key_version: key.version,
        };
        let response = self
            .http
            .post(self.url(&[
                self.config.transit_mount.as_str(),
                "sign",
                key.name.as_str(),
            ])?)
            .header("X-Vault-Token", token)
            .json(&request)
            .send()
            .await
            .map_err(|source| SignError::Http {
                context: "transit_sign",
                source,
            })?;

        let status = response.status();
        let body = response.text().await.map_err(|source| SignError::Http {
            context: "read_transit_sign",
            source,
        })?;
        if !status.is_success() {
            return Err(SignError::Upstream {
                context: "transit_sign",
                status,
                body,
            });
        }

        let response: VaultSignResponse = serde_json::from_str(&body)
            .map_err(|_| SignError::InvalidResponse("data.signature"))?;
        decode_vault_signature(&response.data.signature)
    }

    async fn key_metadata(&self) -> Result<VaultKeyMetadataResponse, SignError> {
        let token = self.vault_token().await?;
        let response = self
            .http
            .get(self.url(&[
                self.config.transit_mount.as_str(),
                "keys",
                self.config.key.name.as_str(),
            ])?)
            .header("X-Vault-Token", token)
            .send()
            .await
            .map_err(|source| SignError::Http {
                context: "transit_key_metadata",
                source,
            })?;

        let status = response.status();
        let body = response.text().await.map_err(|source| SignError::Http {
            context: "read_transit_key_metadata",
            source,
        })?;
        if !status.is_success() {
            return Err(SignError::Upstream {
                context: "transit_key_metadata",
                status,
                body,
            });
        }

        serde_json::from_str(&body).map_err(|_| SignError::InvalidResponse("data.keys"))
    }

    async fn vault_token(&self) -> Result<String, SignError> {
        match &self.config.auth {
            VaultAuthConfig::Token(token) => Ok(token.clone()),
            VaultAuthConfig::KubernetesRole {
                role,
                mount,
                jwt_path,
            } => {
                if let Some(token) = self.role_token_cache.read().await.clone() {
                    return Ok(token);
                }
                let jwt = tokio::fs::read_to_string(jwt_path)
                    .await
                    .map_err(|error| SignError::Auth(error.to_string()))?;
                let request = VaultKubernetesLoginRequest {
                    role,
                    jwt: jwt.trim(),
                };
                let response = self
                    .http
                    .post(self.url(&["auth", mount.as_str(), "login"])?)
                    .json(&request)
                    .send()
                    .await
                    .map_err(|source| SignError::Http {
                        context: "kubernetes_login",
                        source,
                    })?;

                let status = response.status();
                let body = response.text().await.map_err(|source| SignError::Http {
                    context: "read_kubernetes_login",
                    source,
                })?;
                if !status.is_success() {
                    return Err(SignError::Upstream {
                        context: "kubernetes_login",
                        status,
                        body,
                    });
                }

                let response: VaultKubernetesLoginResponse = serde_json::from_str(&body)
                    .map_err(|_| SignError::InvalidResponse("auth.client_token"))?;
                let token = response.auth.client_token;
                *self.role_token_cache.write().await = Some(token.clone());
                Ok(token)
            }
        }
    }

    fn url(&self, segments: &[&str]) -> Result<Url, SignError> {
        let mut url = Url::parse(&self.config.vault_addr)
            .map_err(|error| SignError::Config(format!("invalid VAULT_ADDR: {error}")))?;
        {
            let mut path = url
                .path_segments_mut()
                .map_err(|_| SignError::Config("VAULT_ADDR cannot be a base URL".to_string()))?;
            path.pop_if_empty();
            path.push("v1");
            for segment in segments {
                for part in segment.trim_matches('/').split('/') {
                    if !part.is_empty() {
                        path.push(part);
                    }
                }
            }
        }
        Ok(url)
    }
}

#[async_trait]
impl Signer for VaultTransitSigner {
    fn key_ref(&self) -> VaultKeyRef {
        self.config.key.clone()
    }

    async fn sign(&self, digest: &[u8]) -> Result<Vec<u8>, SignError> {
        self.sign_with_key(&self.config.key, digest).await
    }
}

#[derive(Debug, Serialize)]
struct VaultSignRequest<'a> {
    input: String,
    prehashed: bool,
    hash_algorithm: &'a str,
    signature_algorithm: &'a str,
    key_version: u32,
}

#[derive(Debug, Deserialize)]
struct VaultSignResponse {
    data: VaultSignData,
}

#[derive(Debug, Deserialize)]
struct VaultSignData {
    signature: String,
}

#[derive(Debug, Serialize)]
struct VaultKubernetesLoginRequest<'a> {
    role: &'a str,
    jwt: &'a str,
}

#[derive(Debug, Deserialize)]
struct VaultKubernetesLoginResponse {
    auth: VaultKubernetesLoginAuth,
}

#[derive(Debug, Deserialize)]
struct VaultKubernetesLoginAuth {
    client_token: String,
}

#[derive(Debug, Deserialize)]
struct VaultKeyMetadataResponse {
    data: VaultKeyMetadata,
}

#[derive(Debug, Deserialize)]
struct VaultKeyMetadata {
    latest_version: u32,
    keys: std::collections::HashMap<String, VaultTransitKeyVersion>,
}

#[derive(Debug, Deserialize)]
struct VaultTransitKeyVersion {
    public_key: Option<String>,
}

fn decode_vault_signature(signature: &str) -> Result<Vec<u8>, SignError> {
    let encoded = signature
        .rsplit_once(':')
        .map(|(_, encoded)| encoded)
        .ok_or_else(|| SignError::InvalidSignature(signature.to_string()))?;
    B64.decode(encoded)
        .or_else(|_| URL_SAFE_NO_PAD.decode(encoded))
        .map_err(|error| SignError::InvalidSignature(error.to_string()))
}

fn required_env(key: &str) -> Result<String, SignError> {
    non_empty_env(key).ok_or_else(|| SignError::Config(format!("missing {key}")))
}

fn first_env(keys: &[&str]) -> Option<String> {
    keys.iter().find_map(|key| non_empty_env(key))
}

fn non_empty_env(key: &str) -> Option<String> {
    env::var(key)
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
}

fn optional_env_u64(key: &str) -> Result<Option<u64>, SignError> {
    non_empty_env(key)
        .map(|value| {
            value
                .parse::<u64>()
                .map_err(|error| SignError::Config(format!("invalid {key}: {error}")))
        })
        .transpose()
}

fn optional_env_u32(key: &str) -> Result<Option<u32>, SignError> {
    non_empty_env(key)
        .map(|value| {
            value
                .parse::<u32>()
                .map_err(|error| SignError::Config(format!("invalid {key}: {error}")))
        })
        .transpose()
}

fn optional_env_usize(key: &str) -> Result<Option<usize>, SignError> {
    non_empty_env(key)
        .map(|value| {
            value
                .parse::<usize>()
                .map_err(|error| SignError::Config(format!("invalid {key}: {error}")))
        })
        .transpose()
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
        let rotate_at = activated_at + ChronoDuration::days(self.active_days);
        let retire_at = rotate_at + ChronoDuration::days(self.grace_days);
        (rotate_at, retire_at)
    }

    /// Returns true iff `now` falls inside the dual-publication
    /// window where both the previous and the current key must
    /// appear in JWKS.
    pub fn is_in_grace(&self, prev_activated_at: DateTime<Utc>, now: DateTime<Utc>) -> bool {
        let (rotate_at, retire_at) = self.rotate_and_retire(prev_activated_at);
        now >= rotate_at && now < retire_at
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::{Value, json};
    use uuid::Uuid;
    use wiremock::matchers::{header, method, path};
    use wiremock::{Mock, MockServer, ResponseTemplate};

    fn config(server: &MockServer) -> VaultTransitConfig {
        VaultTransitConfig {
            vault_addr: server.uri(),
            auth: VaultAuthConfig::Token("root-token".into()),
            key: VaultKeyRef {
                name: "of-jwks-active".into(),
                version: 7,
            },
            transit_mount: "transit".into(),
            timeout: Duration::from_secs(1),
            retry_policy: VaultRetryPolicy {
                attempts: 1,
                backoff: Duration::from_millis(1),
            },
            hash_algorithm: "sha2-256".into(),
            signature_algorithm: "pkcs1v15".into(),
        }
    }

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
        let day_89 = activated + ChronoDuration::days(89);
        let day_91 = activated + ChronoDuration::days(91);
        let day_103 = activated + ChronoDuration::days(103);
        let day_105 = activated + ChronoDuration::days(105);

        assert!(!p.is_in_grace(activated, day_89));
        assert!(p.is_in_grace(activated, day_91));
        assert!(p.is_in_grace(activated, day_103));
        assert!(!p.is_in_grace(activated, day_105));
    }

    #[tokio::test]
    async fn signs_prehashed_digest_with_static_token() {
        let server = MockServer::start().await;
        let signature = B64.encode(b"signed-digest");
        Mock::given(method("POST"))
            .and(path("/v1/transit/sign/of-jwks-active"))
            .and(header("X-Vault-Token", "root-token"))
            .respond_with(ResponseTemplate::new(200).set_body_json(json!({
                "data": { "signature": format!("vault:v7:{signature}") }
            })))
            .mount(&server)
            .await;

        let signer = VaultTransitSigner::new(config(&server)).unwrap();
        let signed = signer.sign(b"digest").await.unwrap();
        assert_eq!(signed, b"signed-digest");

        let requests = server.received_requests().await.unwrap();
        let body: Value = serde_json::from_slice(&requests[0].body).unwrap();
        assert_eq!(body["input"], B64.encode(b"digest"));
        assert_eq!(body["prehashed"], true);
        assert_eq!(body["hash_algorithm"], "sha2-256");
        assert_eq!(body["signature_algorithm"], "pkcs1v15");
        assert_eq!(body["key_version"], 7);
    }

    #[tokio::test]
    async fn retries_retryable_vault_errors() {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/v1/transit/sign/of-jwks-active"))
            .respond_with(ResponseTemplate::new(503).set_body_string("maintenance"))
            .expect(2)
            .mount(&server)
            .await;

        let mut cfg = config(&server);
        cfg.retry_policy.attempts = 2;
        let signer = VaultTransitSigner::new(cfg).unwrap();
        let error = signer.sign(b"digest").await.unwrap_err();
        assert!(matches!(
            error,
            SignError::Upstream {
                status: StatusCode::SERVICE_UNAVAILABLE,
                ..
            }
        ));
    }

    #[tokio::test]
    async fn kubernetes_role_auth_logs_in_and_caches_client_token() {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/v1/auth/kubernetes/login"))
            .respond_with(ResponseTemplate::new(200).set_body_json(json!({
                "auth": { "client_token": "role-token" }
            })))
            .expect(1)
            .mount(&server)
            .await;
        let signature = B64.encode(b"role-signed");
        Mock::given(method("POST"))
            .and(path("/v1/transit/sign/of-jwks-active"))
            .and(header("X-Vault-Token", "role-token"))
            .respond_with(ResponseTemplate::new(200).set_body_json(json!({
                "data": { "signature": format!("vault:v7:{signature}") }
            })))
            .expect(2)
            .mount(&server)
            .await;

        let jwt_path = env::temp_dir().join(format!("of-vault-jwt-{}", Uuid::new_v4()));
        std::fs::write(&jwt_path, "service-account-jwt\n").unwrap();
        let mut cfg = config(&server);
        cfg.auth = VaultAuthConfig::KubernetesRole {
            role: "identity-federation".into(),
            mount: "kubernetes".into(),
            jwt_path: jwt_path.to_string_lossy().to_string(),
        };
        let signer = VaultTransitSigner::new(cfg).unwrap();

        assert_eq!(signer.sign(b"first").await.unwrap(), b"role-signed");
        assert_eq!(signer.sign(b"second").await.unwrap(), b"role-signed");
        let _ = std::fs::remove_file(jwt_path);
    }

    #[tokio::test]
    async fn rotates_key_and_reads_public_key_from_metadata() {
        let server = MockServer::start().await;
        Mock::given(method("POST"))
            .and(path("/v1/transit/keys/of-jwks-active/rotate"))
            .and(header("X-Vault-Token", "root-token"))
            .respond_with(ResponseTemplate::new(204))
            .mount(&server)
            .await;
        Mock::given(method("GET"))
            .and(path("/v1/transit/keys/of-jwks-active"))
            .and(header("X-Vault-Token", "root-token"))
            .respond_with(ResponseTemplate::new(200).set_body_json(json!({
                "data": {
                    "latest_version": 8,
                    "keys": {
                        "7": { "public_key": "-----BEGIN PUBLIC KEY-----\nv7\n-----END PUBLIC KEY-----" },
                        "8": { "public_key": "-----BEGIN PUBLIC KEY-----\nv8\n-----END PUBLIC KEY-----" }
                    }
                }
            })))
            .mount(&server)
            .await;

        let signer = VaultTransitSigner::new(config(&server)).unwrap();
        let rotated = signer.rotate_key().await.unwrap();
        assert_eq!(rotated.version, 8);
        let public_key = signer.public_key_pem(&rotated).await.unwrap();
        assert!(public_key.contains("v8"));
    }
}
