use std::path::Path;
use std::{env, fs, io};

use jsonwebtoken::{
    Algorithm, DecodingKey, EncodingKey, Header, Validation, decode, decode_header, encode,
};
use rand::RngCore;
use serde_json::Value;
use thiserror::Error;
use uuid::Uuid;

use crate::claims::{Claims, SessionScope};

/// Number of random bytes used by the auto-generated HS256 secret. 32 bytes
/// (256 bits) matches the recommended minimum for HMAC-SHA256.
const GENERATED_SECRET_BYTES: usize = 32;

const JWT_SECRET_ENV_KEYS: &[&str] = &["OPENFOUNDRY_JWT_SECRET", "JWT_SECRET"];
const JWT_SECRET_PATH_ENV_KEYS: &[&str] = &["OPENFOUNDRY_JWT_SECRET_PATH", "JWT_SECRET_PATH"];
const JWT_ISSUER_ENV_KEYS: &[&str] = &["OPENFOUNDRY_JWT_ISSUER", "JWT_ISSUER"];
const JWT_AUDIENCE_ENV_KEYS: &[&str] = &["OPENFOUNDRY_JWT_AUDIENCE", "JWT_AUDIENCE"];
const JWT_KEY_ID_ENV_KEYS: &[&str] = &["OPENFOUNDRY_JWT_KID", "JWT_KID"];
const JWT_PRIVATE_KEY_ENV_KEYS: &[&str] =
    &["OPENFOUNDRY_JWT_PRIVATE_KEY_PEM", "JWT_PRIVATE_KEY_PEM"];
const JWT_PRIVATE_KEY_PATH_ENV_KEYS: &[&str] =
    &["OPENFOUNDRY_JWT_PRIVATE_KEY_PATH", "JWT_PRIVATE_KEY_PATH"];
const JWT_PUBLIC_KEY_ENV_KEYS: &[&str] = &["OPENFOUNDRY_JWT_PUBLIC_KEY_PEM", "JWT_PUBLIC_KEY_PEM"];
const JWT_PUBLIC_KEY_PATH_ENV_KEYS: &[&str] =
    &["OPENFOUNDRY_JWT_PUBLIC_KEY_PATH", "JWT_PUBLIC_KEY_PATH"];

#[derive(Debug, Error)]
pub enum JwtError {
    #[error("token expired")]
    Expired,
    #[error("invalid token: {0}")]
    Invalid(String),
    #[error("encoding error: {0}")]
    Encoding(String),
}

/// Configuration for JWT token operations.
#[derive(Debug, Clone)]
pub struct JwtConfig {
    /// Secret key for HMAC-SHA256 signing.
    secret: Vec<u8>,
    /// Optional token issuer applied to issued tokens and required during validation.
    issuer: Option<String>,
    /// Optional token audience applied to issued tokens and required during validation.
    audience: Option<String>,
    /// Optional JOSE key id embedded in JWT headers.
    key_id: Option<String>,
    /// Optional PEM-encoded RSA private key used for RS256 signing.
    rsa_private_key_pem: Option<String>,
    /// Optional PEM-encoded RSA public key used for RS256 verification.
    rsa_public_key_pem: Option<String>,
    /// Access token TTL in seconds (default: 3600).
    pub access_ttl_secs: i64,
    /// Refresh token TTL in seconds (default: 604800 = 7 days).
    pub refresh_ttl_secs: i64,
}

impl JwtConfig {
    pub fn new(secret: &str) -> Self {
        Self {
            secret: secret.as_bytes().to_vec(),
            issuer: None,
            audience: None,
            key_id: None,
            rsa_private_key_pem: None,
            rsa_public_key_pem: None,
            access_ttl_secs: 3600,
            refresh_ttl_secs: 604_800,
        }
    }

    /// Build a [`JwtConfig`] backed by a freshly generated, cryptographically
    /// random 256-bit HS256 secret.
    ///
    /// The secret only lives in memory for the lifetime of the returned value
    /// and is NOT persisted, so tokens issued with it become invalid after a
    /// process restart. For long-lived deployments prefer
    /// [`JwtConfig::load_or_generate`] or [`JwtConfig::resolve_unattended`],
    /// which persist the generated secret to disk so unattended restarts keep
    /// existing tokens valid.
    ///
    /// This constructor is the safe replacement for hard-coded test literals
    /// such as `JwtConfig::new("secret")`.
    pub fn generate() -> Self {
        Self::from_secret_bytes(generate_secret_bytes())
    }

    /// Build a [`JwtConfig`] from an existing byte slice secret.
    ///
    /// Useful for callers that source the secret from a key-management system
    /// where the raw bytes are already available.
    pub fn from_secret_bytes(secret: impl Into<Vec<u8>>) -> Self {
        Self {
            secret: secret.into(),
            issuer: None,
            audience: None,
            key_id: None,
            rsa_private_key_pem: None,
            rsa_public_key_pem: None,
            access_ttl_secs: 3600,
            refresh_ttl_secs: 604_800,
        }
    }

    /// Load an HS256 secret from `path`, generating and persisting a new
    /// random secret on first use so deployments can start fully unattended.
    ///
    /// On Unix the persisted file is created with mode `0600` so it is only
    /// readable by the owning process. The parent directory is created with
    /// mode `0700` if it does not already exist. Subsequent restarts read the
    /// same secret from disk so tokens issued before the restart remain valid.
    pub fn load_or_generate(path: impl AsRef<Path>) -> Result<Self, SecretLoadError> {
        let secret = load_or_generate_secret(path.as_ref())?;
        Ok(Self::from_secret_bytes(secret))
    }

    /// Resolve the HS256 secret following an unattended precedence:
    ///
    /// 1. `OPENFOUNDRY_JWT_SECRET` / `JWT_SECRET` (raw secret in the env).
    /// 2. `OPENFOUNDRY_JWT_SECRET_PATH` / `JWT_SECRET_PATH` (file path).
    /// 3. The supplied `default_path`, auto-generated if missing.
    ///
    /// This lets operators inject a managed secret when one is available
    /// while still allowing the service to boot with no configuration.
    pub fn resolve_unattended(default_path: impl AsRef<Path>) -> Result<Self, SecretLoadError> {
        if let Some(secret) = read_first_env(JWT_SECRET_ENV_KEYS) {
            return Ok(Self::from_secret_bytes(secret.into_bytes()));
        }

        let path = read_first_env(JWT_SECRET_PATH_ENV_KEYS)
            .map(std::path::PathBuf::from)
            .unwrap_or_else(|| default_path.as_ref().to_path_buf());
        Self::load_or_generate(path)
    }

    pub fn with_access_ttl(mut self, secs: i64) -> Self {
        self.access_ttl_secs = secs;
        self
    }

    pub fn with_refresh_ttl(mut self, secs: i64) -> Self {
        self.refresh_ttl_secs = secs;
        self
    }

    pub fn with_issuer(mut self, issuer: impl Into<String>) -> Self {
        self.issuer = Some(issuer.into());
        self
    }

    pub fn with_audience(mut self, audience: impl Into<String>) -> Self {
        self.audience = Some(audience.into());
        self
    }

    pub fn with_key_id(mut self, key_id: impl Into<String>) -> Self {
        self.key_id = Some(key_id.into());
        self
    }

    pub fn with_rsa_keys(
        mut self,
        private_key_pem: impl Into<String>,
        public_key_pem: impl Into<String>,
    ) -> Self {
        self.rsa_private_key_pem = Some(private_key_pem.into());
        self.rsa_public_key_pem = Some(public_key_pem.into());
        self
    }

    pub fn with_env_defaults(mut self) -> Self {
        if let Some(issuer) = read_first_env(JWT_ISSUER_ENV_KEYS) {
            self = self.with_issuer(issuer);
        }
        if let Some(audience) = read_first_env(JWT_AUDIENCE_ENV_KEYS) {
            self = self.with_audience(audience);
        }
        if let Some(key_id) = read_first_env(JWT_KEY_ID_ENV_KEYS) {
            self = self.with_key_id(key_id);
        }

        let private_key =
            read_pem_from_env(JWT_PRIVATE_KEY_ENV_KEYS, JWT_PRIVATE_KEY_PATH_ENV_KEYS);
        let public_key = read_pem_from_env(JWT_PUBLIC_KEY_ENV_KEYS, JWT_PUBLIC_KEY_PATH_ENV_KEYS);

        match (private_key, public_key) {
            (Some(private_key), Some(public_key)) => self.with_rsa_keys(private_key, public_key),
            (Some(_), None) | (None, Some(_)) => {
                tracing::warn!(
                    "partial JWT RSA configuration detected; falling back to shared-secret HS256"
                );
                self
            }
            (None, None) => self,
        }
    }

    pub fn issuer(&self) -> Option<&str> {
        self.issuer.as_deref()
    }

    pub fn audience(&self) -> Option<&str> {
        self.audience.as_deref()
    }

    fn algorithm(&self) -> Algorithm {
        if self.rsa_private_key_pem.is_some() && self.rsa_public_key_pem.is_some() {
            Algorithm::RS256
        } else {
            Algorithm::HS256
        }
    }

    fn encoding_key(&self) -> Result<EncodingKey, JwtError> {
        match self.algorithm() {
            Algorithm::HS256 => Ok(EncodingKey::from_secret(&self.secret)),
            Algorithm::RS256 => self
                .rsa_private_key_pem
                .as_ref()
                .ok_or_else(|| JwtError::Encoding("missing RSA private key".to_string()))
                .and_then(|pem| {
                    EncodingKey::from_rsa_pem(pem.as_bytes())
                        .map_err(|error| JwtError::Encoding(error.to_string()))
                }),
            other => Err(JwtError::Encoding(format!(
                "unsupported signing algorithm {other:?}"
            ))),
        }
    }

    fn decoding_key(&self) -> Result<DecodingKey, JwtError> {
        match self.algorithm() {
            Algorithm::HS256 => Ok(DecodingKey::from_secret(&self.secret)),
            Algorithm::RS256 => self
                .rsa_public_key_pem
                .as_ref()
                .ok_or_else(|| JwtError::Invalid("missing RSA public key".to_string()))
                .and_then(|pem| {
                    DecodingKey::from_rsa_pem(pem.as_bytes())
                        .map_err(|error| JwtError::Invalid(error.to_string()))
                }),
            other => Err(JwtError::Invalid(format!(
                "unsupported signing algorithm {other:?}"
            ))),
        }
    }
}

/// Encode claims into a signed JWT string.
pub fn encode_token(config: &JwtConfig, claims: &Claims) -> Result<String, JwtError> {
    let mut header = Header::new(config.algorithm());
    header.kid = config.key_id.clone();

    encode(&header, claims, &config.encoding_key()?).map_err(|e| JwtError::Encoding(e.to_string()))
}

/// Decode and validate a JWT string into Claims.
pub fn decode_token(config: &JwtConfig, token: &str) -> Result<Claims, JwtError> {
    let header = decode_header(token).map_err(|error| JwtError::Invalid(error.to_string()))?;
    let algorithm = config.algorithm();
    if header.alg != algorithm {
        return Err(JwtError::Invalid(format!(
            "unexpected signing algorithm {:?}",
            header.alg
        )));
    }

    let mut validation = Validation::new(algorithm);
    validation.validate_exp = true;
    validation.leeway = 30; // 30 second leeway for clock skew

    if let Some(issuer) = config.issuer() {
        validation.set_issuer(&[issuer]);
    }
    if let Some(audience) = config.audience() {
        validation.set_audience(&[audience]);
    }

    let data =
        decode::<Claims>(token, &config.decoding_key()?, &validation).map_err(|e| {
            match e.kind() {
                jsonwebtoken::errors::ErrorKind::ExpiredSignature => JwtError::Expired,
                _ => JwtError::Invalid(e.to_string()),
            }
        })?;

    Ok(data.claims)
}

/// Build a new Claims set for a user (access token).
pub fn build_access_claims(
    config: &JwtConfig,
    user_id: Uuid,
    email: &str,
    name: &str,
    roles: Vec<String>,
    permissions: Vec<String>,
    org_id: Option<Uuid>,
    attributes: Value,
    auth_methods: Vec<String>,
) -> Claims {
    build_access_claims_with_scope(
        config,
        user_id,
        email,
        name,
        roles,
        permissions,
        org_id,
        attributes,
        auth_methods,
        None,
        Some("access".to_string()),
    )
}

pub fn build_access_claims_with_scope(
    config: &JwtConfig,
    user_id: Uuid,
    email: &str,
    name: &str,
    roles: Vec<String>,
    permissions: Vec<String>,
    org_id: Option<Uuid>,
    attributes: Value,
    auth_methods: Vec<String>,
    session_scope: Option<SessionScope>,
    session_kind: Option<String>,
) -> Claims {
    let now = chrono::Utc::now().timestamp();
    base_claims(
        config,
        user_id,
        now,
        now + config.access_ttl_secs,
        email.to_string(),
        name.to_string(),
        roles,
        permissions,
        org_id,
        attributes,
        auth_methods,
        Some("access".to_string()),
        None,
        session_kind,
        session_scope,
    )
}

/// Build a minimal Claims set for a refresh token.
pub fn build_refresh_claims(config: &JwtConfig, user_id: Uuid) -> Claims {
    let now = chrono::Utc::now().timestamp();
    base_claims(
        config,
        user_id,
        now,
        now + config.refresh_ttl_secs,
        String::new(),
        String::new(),
        vec![],
        vec![],
        None,
        Value::Object(Default::default()),
        vec![],
        Some("refresh".to_string()),
        None,
        None,
        None,
    )
}

/// Build claims for a long-lived API key.
pub fn build_api_key_claims(
    config: &JwtConfig,
    user_id: Uuid,
    email: &str,
    name: &str,
    roles: Vec<String>,
    permissions: Vec<String>,
    org_id: Option<Uuid>,
    attributes: Value,
    api_key_id: Uuid,
    expires_in_secs: i64,
) -> Claims {
    build_api_key_claims_with_scope(
        config,
        user_id,
        email,
        name,
        roles,
        permissions,
        org_id,
        attributes,
        api_key_id,
        expires_in_secs,
        None,
        None,
    )
}

pub fn build_api_key_claims_with_scope(
    config: &JwtConfig,
    user_id: Uuid,
    email: &str,
    name: &str,
    roles: Vec<String>,
    permissions: Vec<String>,
    org_id: Option<Uuid>,
    attributes: Value,
    api_key_id: Uuid,
    expires_in_secs: i64,
    session_scope: Option<SessionScope>,
    session_kind: Option<String>,
) -> Claims {
    let now = chrono::Utc::now().timestamp();
    base_claims(
        config,
        user_id,
        now,
        now + expires_in_secs,
        email.to_string(),
        name.to_string(),
        roles,
        permissions,
        org_id,
        attributes,
        vec!["api_key".to_string()],
        Some("api_key".to_string()),
        Some(api_key_id),
        session_kind,
        session_scope,
    )
}

fn base_claims(
    config: &JwtConfig,
    sub: Uuid,
    iat: i64,
    exp: i64,
    email: String,
    name: String,
    roles: Vec<String>,
    permissions: Vec<String>,
    org_id: Option<Uuid>,
    attributes: Value,
    auth_methods: Vec<String>,
    token_use: Option<String>,
    api_key_id: Option<Uuid>,
    session_kind: Option<String>,
    session_scope: Option<SessionScope>,
) -> Claims {
    Claims {
        sub,
        iat,
        exp,
        iss: config.issuer.clone(),
        aud: config.audience.clone(),
        jti: api_key_id.unwrap_or_else(Uuid::now_v7),
        email,
        name,
        roles,
        permissions,
        org_id,
        attributes,
        auth_methods,
        token_use,
        api_key_id,
        session_kind,
        session_scope,
    }
}

fn read_first_env(keys: &[&str]) -> Option<String> {
    keys.iter().find_map(|key| read_env(key))
}

fn read_env(key: &str) -> Option<String> {
    env::var(key)
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
}

fn read_pem_from_env(value_keys: &[&str], path_keys: &[&str]) -> Option<String> {
    read_first_env(value_keys)
        .map(|value| value.replace("\\n", "\n"))
        .or_else(|| {
            read_first_env(path_keys).and_then(|path| {
                fs::read_to_string(path)
                    .ok()
                    .map(|value| value.trim().to_string())
                    .filter(|value| !value.is_empty())
            })
        })
}

/// Errors returned by [`JwtConfig::load_or_generate`] and
/// [`JwtConfig::resolve_unattended`].
#[derive(Debug, Error)]
pub enum SecretLoadError {
    #[error("failed to access JWT secret at {path}: {source}")]
    Io {
        path: std::path::PathBuf,
        #[source]
        source: io::Error,
    },
    #[error("JWT secret file at {path} is empty")]
    Empty { path: std::path::PathBuf },
}

fn generate_secret_bytes() -> Vec<u8> {
    let mut bytes = vec![0u8; GENERATED_SECRET_BYTES];
    rand::thread_rng().fill_bytes(&mut bytes);
    bytes
}

fn load_or_generate_secret(path: &Path) -> Result<Vec<u8>, SecretLoadError> {
    match fs::read(path) {
        Ok(bytes) => {
            // Trim trailing whitespace / newlines that an operator may have
            // introduced when writing the file by hand. The on-disk format is
            // hex; leading/trailing whitespace is never significant.
            let trimmed = trim_ascii_whitespace(&bytes);
            if trimmed.is_empty() {
                return Err(SecretLoadError::Empty {
                    path: path.to_path_buf(),
                });
            }
            // Accept both hex-encoded and raw byte payloads. Hex is preferred
            // (it is what we write ourselves), but we tolerate raw bytes so a
            // secret seeded by a key-management system also works.
            match decode_hex(trimmed) {
                Some(decoded) if !decoded.is_empty() => Ok(decoded),
                _ => Ok(trimmed.to_vec()),
            }
        }
        Err(error) if error.kind() == io::ErrorKind::NotFound => {
            let bytes = generate_secret_bytes();
            persist_secret(path, &bytes).map_err(|source| SecretLoadError::Io {
                path: path.to_path_buf(),
                source,
            })?;
            tracing::warn!(
                path = %path.display(),
                "generated new JWT signing secret; persisted for unattended restarts"
            );
            Ok(bytes)
        }
        Err(source) => Err(SecretLoadError::Io {
            path: path.to_path_buf(),
            source,
        }),
    }
}

fn persist_secret(path: &Path, bytes: &[u8]) -> io::Result<()> {
    if let Some(parent) = path.parent() {
        if !parent.as_os_str().is_empty() {
            create_dir_all_secure(parent)?;
        }
    }
    let encoded = encode_hex(bytes);
    write_secure(path, encoded.as_bytes())
}

#[cfg(unix)]
fn create_dir_all_secure(dir: &Path) -> io::Result<()> {
    use std::os::unix::fs::DirBuilderExt;
    if dir.exists() {
        return Ok(());
    }
    fs::DirBuilder::new()
        .recursive(true)
        .mode(0o700)
        .create(dir)
}

#[cfg(not(unix))]
fn create_dir_all_secure(dir: &Path) -> io::Result<()> {
    fs::create_dir_all(dir)
}

#[cfg(unix)]
fn write_secure(path: &Path, contents: &[u8]) -> io::Result<()> {
    use std::io::Write;
    use std::os::unix::fs::OpenOptionsExt;

    let mut file = fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(path)?;
    file.write_all(contents)?;
    file.sync_all()
}

#[cfg(not(unix))]
fn write_secure(path: &Path, contents: &[u8]) -> io::Result<()> {
    fs::write(path, contents)
}

fn trim_ascii_whitespace(bytes: &[u8]) -> &[u8] {
    let start = bytes
        .iter()
        .position(|byte| !byte.is_ascii_whitespace())
        .unwrap_or(bytes.len());
    let end = bytes
        .iter()
        .rposition(|byte| !byte.is_ascii_whitespace())
        .map(|idx| idx + 1)
        .unwrap_or(start);
    &bytes[start..end]
}

fn encode_hex(bytes: &[u8]) -> String {
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        out.push(hex_digit(byte >> 4));
        out.push(hex_digit(byte & 0x0f));
    }
    out
}

fn hex_digit(nibble: u8) -> char {
    debug_assert!(nibble < 16, "nibble out of range");
    match nibble {
        0..=9 => (b'0' + nibble) as char,
        _ => (b'a' + (nibble & 0x0f) - 10) as char,
    }
}

fn decode_hex(bytes: &[u8]) -> Option<Vec<u8>> {
    if bytes.is_empty() || bytes.len() % 2 != 0 {
        return None;
    }
    let mut out = Vec::with_capacity(bytes.len() / 2);
    for chunk in bytes.chunks_exact(2) {
        let hi = hex_value(chunk[0])?;
        let lo = hex_value(chunk[1])?;
        out.push((hi << 4) | lo);
    }
    Some(out)
}

fn hex_value(byte: u8) -> Option<u8> {
    match byte {
        b'0'..=b'9' => Some(byte - b'0'),
        b'a'..=b'f' => Some(byte - b'a' + 10),
        b'A'..=b'F' => Some(byte - b'A' + 10),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use serde_json::json;

    use super::*;

    const RSA_PRIVATE_KEY: &str = r#"-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQDgoWcspyon7gi7
/8v8jQSOFsf4DuA/fnl+bVSfLI9mGNlNqr0YZg4ugjq/NMS06tHAXaT7UlBGCiOq
DDo+PmWBBSf3rZB0DZNH+sF63SJsB2N7YXBG4xg2TWG9CRQomaVlptIqoaRAT/Ot
uPJ2GPxtdHXRfHBqnkA81mxxTxoV3UUtJIeo5WatAA3r+uHcPMjCZuIQDbXc+5Sg
//l6OQivKI4QguOv4BVgbBvIQA9WVEVcO8Na7QVp8fAccTffB32xyVH21WSsZe8P
ourdegntMsJio5SKv1l7yVxJoOD+K2owdPj6bSxe2EaxfrulmirIGs2lOeX6j4qR
Y0zqvHQZAgMBAAECggEARFIzDEzHsJ9gfrW9eFH3ybO6HIOBxy4Ti9V7AHLQJrB2
H35Hx0z7EUBA1/kXvyMQqt6QmHQfwD3DPSw85sOZodVMo7NhlTqvyhvFjzYFCzBw
HI21VYoqyhFdId7KB9M7kCBeGeNSDtGCfxsae7r7w9rBHvcnRfZd+WMKVqhFedJh
8YVHL+IV5iw01/9i7PkR4ChD4Oc1W0Lw4pXAy8v0xhOqrF7M1mHUZP5tmDdfaVzC
0YIuL6axqX/ZBJMUfiyAe3/7PUE9f4uwqsddYcoMDAV2l0TjXUsFCqAhz1ubW48l
WtiGZy39eo9ybM5LLWnEzIYpiGvaC+qwozJWEvsvFQKBgQD8PIDRsXlNoil1wmNn
qYrHLPt+zwyst0csOZXLkHpvMuHcEli1DhoCcNqwEGZoJCXngNFpaLNGKdBrG9xa
sq98rnwUFZjXGJnHkLUuxgc7fLZnePG0xLyzDJ4/dgqMvez8GqFEtWIRuL0T2Qta
G59Lxz9D3691Gpi1ZoOa1zggmwKBgQDj+3NScZ7A5bh25XtxJuSFDgbJsA56opLY
3RMI104c7vIj2IQKyOKQCr8Ez8Orrn/XJ+8m8t6IKg9g6G9zjIZ2KryYFhELRla6
T63C9ewzZcSkAYZ7dExQPIawzCvjyE8qY8khlJnR9o5PknW3+e7so42yJIKP6BWY
8q+YUmfnWwKBgCRusMSY98Zo18g0jZsZd/wQ2TqVuWTxDAytPJ+sfKK3HLxmwf1U
zhjwKAYqOEBuiDMJ/jVVdB98RqhR2+AV0xcVNMLJ48uduAiFNEZPQBgtiUMkyvSr
Pf42olzUNe3iOOqpBgYglMuufVDylpsrRjTx0IeDNZqaftgkuHmTAH5lAoGBAM/Q
+oKAh9IWlVvsO+YdKdoPuyhGkCxB3dJJU3yPpujA94CtcU/TZpMe+JkOOrNY0bfy
8xFx+l/s1y/jMRUHV9qHgnqwQsEgURZsY1yAh9siPWmy6j/G93l8ctregnOUuHVP
mJw/tSertHXcb+pQrfaP8C4fEdTUHjvZnS8gjw5ZAoGBAK2zkSk40SawCkYep/oy
Z3X6pg60JV8Sa/vyXifzzY4uBi5ByaTc9OTcxQcfxRzz8rCoy7nF101Pipotn37F
wK1X7yzmEwEi2GctHWyyPKFTpFpmjTH4gG7uTfF3cHztqufg6rRPWGh6qRMMRFm6
6dQUlev76ajL1zziuySGpdmm
-----END PRIVATE KEY-----"#;
    const RSA_PUBLIC_KEY: &str = r#"-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA4KFnLKcqJ+4Iu//L/I0E
jhbH+A7gP355fm1UnyyPZhjZTaq9GGYOLoI6vzTEtOrRwF2k+1JQRgojqgw6Pj5l
gQUn962QdA2TR/rBet0ibAdje2FwRuMYNk1hvQkUKJmlZabSKqGkQE/zrbjydhj8
bXR10Xxwap5APNZscU8aFd1FLSSHqOVmrQAN6/rh3DzIwmbiEA213PuUoP/5ejkI
ryiOEILjr+AVYGwbyEAPVlRFXDvDWu0FafHwHHE33wd9sclR9tVkrGXvD6Lq3XoJ
7TLCYqOUir9Ze8lcSaDg/itqMHT4+m0sXthGsX67pZoqyBrNpTnl+o+KkWNM6rx0
GQIDAQAB
-----END PUBLIC KEY-----"#;

    #[test]
    fn rs256_tokens_round_trip_with_standard_claims() {
        let config = JwtConfig::generate()
            .with_rsa_keys(RSA_PRIVATE_KEY, RSA_PUBLIC_KEY)
            .with_issuer("https://auth.openfoundry.test")
            .with_audience("openfoundry")
            .with_key_id("test-key");

        let claims = build_access_claims(
            &config,
            Uuid::now_v7(),
            "demo@example.com",
            "Demo User",
            vec!["member".to_string()],
            vec!["datasets:read".to_string()],
            Some(Uuid::now_v7()),
            json!({ "region": "eu" }),
            vec!["password".to_string()],
        );

        let token = encode_token(&config, &claims).expect("token should encode");
        let header = decode_header(&token).expect("header should decode");
        let decoded = decode_token(&config, &token).expect("token should decode");

        assert_eq!(header.alg, Algorithm::RS256);
        assert_eq!(header.kid.as_deref(), Some("test-key"));
        assert_eq!(
            decoded.iss.as_deref(),
            Some("https://auth.openfoundry.test")
        );
        assert_eq!(decoded.aud.as_deref(), Some("openfoundry"));
        assert_eq!(decoded.email, "demo@example.com");
    }

    #[test]
    fn rejects_tokens_with_wrong_audience() {
        let issuer_config = JwtConfig::generate()
            .with_rsa_keys(RSA_PRIVATE_KEY, RSA_PUBLIC_KEY)
            .with_issuer("https://auth.openfoundry.test")
            .with_audience("openfoundry");
        let verifier_config = issuer_config.clone().with_audience("someone-else");

        let claims = build_refresh_claims(&issuer_config, Uuid::now_v7());
        let token = encode_token(&issuer_config, &claims).expect("token should encode");
        let error = decode_token(&verifier_config, &token).expect_err("token should be rejected");

        assert!(matches!(error, JwtError::Invalid(_)));
    }

    #[test]
    fn generate_produces_distinct_random_secrets() {
        let first = JwtConfig::generate();
        let second = JwtConfig::generate();
        assert_eq!(first.secret.len(), GENERATED_SECRET_BYTES);
        assert_eq!(second.secret.len(), GENERATED_SECRET_BYTES);
        assert_ne!(first.secret, second.secret);
    }

    #[test]
    fn load_or_generate_persists_and_reuses_secret() {
        let dir = std::env::temp_dir().join(format!("openfoundry-jwt-{}", Uuid::now_v7()));
        let path = dir.join("jwt.secret");

        let first = JwtConfig::load_or_generate(&path).expect("first load should succeed");
        let second = JwtConfig::load_or_generate(&path).expect("second load should succeed");
        assert_eq!(first.secret, second.secret);
        assert_eq!(first.secret.len(), GENERATED_SECRET_BYTES);

        // The on-disk format is hex-encoded, never the raw bytes.
        let on_disk = fs::read(&path).expect("secret file should exist");
        assert_eq!(on_disk.len(), GENERATED_SECRET_BYTES * 2);
        assert!(on_disk.iter().all(|byte| byte.is_ascii_hexdigit()));

        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn load_or_generate_rejects_empty_secret_file() {
        let dir = std::env::temp_dir().join(format!("openfoundry-jwt-empty-{}", Uuid::now_v7()));
        fs::create_dir_all(&dir).expect("dir");
        let path = dir.join("jwt.secret");
        fs::write(&path, b"   \n").expect("write");

        let error = JwtConfig::load_or_generate(&path).expect_err("empty file should be rejected");
        assert!(matches!(error, SecretLoadError::Empty { .. }));

        let _ = fs::remove_dir_all(&dir);
    }
}
