//! Symmetric envelope encryption used by `set_credential` to protect the
//! plaintext secret at rest. The MVP uses AES-256-GCM with a 12-byte random
//! nonce per record. Layout in the `secret_ciphertext` BYTEA column is:
//!
//! ```text
//! [ version: u8 = 1 ][ nonce: 12 bytes ][ ciphertext+tag: variable ]
//! ```
//!
//! The data-encryption key is loaded once from the
//! `CREDENTIAL_ENCRYPTION_KEY` env var (32-byte raw key, base64-encoded). When
//! the var is unset the service falls back to deriving a key from
//! `JWT_SECRET` via SHA-256 so dev environments keep working without extra
//! configuration; production deployments MUST set the dedicated key.

use aes_gcm::aead::{Aead, OsRng};
use aes_gcm::{AeadCore, Aes256Gcm, Key, KeyInit};
use base64::Engine;
use sha2::{Digest, Sha256};

const VERSION: u8 = 1;
const NONCE_LEN: usize = 12;

#[derive(Debug, thiserror::Error)]
pub enum CredentialCryptoError {
    #[error("encryption failed: {0}")]
    Encrypt(String),
    #[error("decryption failed: {0}")]
    Decrypt(String),
    #[error("ciphertext blob is malformed (len={0})")]
    Malformed(usize),
    #[error("unsupported credential version: {0}")]
    UnsupportedVersion(u8),
    #[error("CREDENTIAL_ENCRYPTION_KEY must decode to 32 bytes (got {0})")]
    BadKeyLength(usize),
}

/// Derive a 32-byte data-encryption key from process configuration.
pub fn derive_key(
    env_key_b64: Option<&str>,
    jwt_secret: &str,
) -> Result<[u8; 32], CredentialCryptoError> {
    if let Some(b64) = env_key_b64.map(str::trim).filter(|s| !s.is_empty()) {
        let raw = base64::engine::general_purpose::STANDARD
            .decode(b64)
            .map_err(|err| CredentialCryptoError::Encrypt(format!("base64 decode key: {err}")))?;
        if raw.len() != 32 {
            return Err(CredentialCryptoError::BadKeyLength(raw.len()));
        }
        let mut out = [0u8; 32];
        out.copy_from_slice(&raw);
        Ok(out)
    } else {
        // Dev fallback: SHA-256 of the JWT secret. Logged once at startup so
        // operators understand they are running on the dev key.
        let mut hasher = Sha256::new();
        hasher.update(b"openfoundry/credential-encryption/v1\0");
        hasher.update(jwt_secret.as_bytes());
        let digest = hasher.finalize();
        let mut out = [0u8; 32];
        out.copy_from_slice(&digest);
        Ok(out)
    }
}

pub fn encrypt(key: &[u8; 32], plaintext: &[u8]) -> Result<Vec<u8>, CredentialCryptoError> {
    let cipher = Aes256Gcm::new(Key::<Aes256Gcm>::from_slice(key));
    let nonce = Aes256Gcm::generate_nonce(&mut OsRng);
    let mut ct = cipher
        .encrypt(&nonce, plaintext)
        .map_err(|err| CredentialCryptoError::Encrypt(err.to_string()))?;
    let mut out = Vec::with_capacity(1 + NONCE_LEN + ct.len());
    out.push(VERSION);
    out.extend_from_slice(nonce.as_slice());
    out.append(&mut ct);
    Ok(out)
}

pub fn decrypt(key: &[u8; 32], blob: &[u8]) -> Result<Vec<u8>, CredentialCryptoError> {
    if blob.len() < 1 + NONCE_LEN {
        return Err(CredentialCryptoError::Malformed(blob.len()));
    }
    let version = blob[0];
    if version != VERSION {
        return Err(CredentialCryptoError::UnsupportedVersion(version));
    }
    let nonce_bytes: [u8; NONCE_LEN] = blob[1..1 + NONCE_LEN]
        .try_into()
        .map_err(|_| CredentialCryptoError::Malformed(blob.len()))?;
    let cipher = Aes256Gcm::new(Key::<Aes256Gcm>::from_slice(key));
    cipher
        .decrypt(&nonce_bytes.into(), &blob[1 + NONCE_LEN..])
        .map_err(|err| CredentialCryptoError::Decrypt(err.to_string()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trip() {
        let key = derive_key(None, "openfoundry-dev-secret").unwrap();
        let plaintext = b"super-secret";
        let blob = encrypt(&key, plaintext).unwrap();
        assert_ne!(&blob[1 + NONCE_LEN..], plaintext);
        assert_eq!(decrypt(&key, &blob).unwrap(), plaintext);
    }

    #[test]
    fn rejects_truncated_blob() {
        let key = derive_key(None, "x").unwrap();
        let err = decrypt(&key, &[1, 2, 3]).unwrap_err();
        assert!(matches!(err, CredentialCryptoError::Malformed(_)));
    }

    #[test]
    fn rejects_wrong_version() {
        let key = derive_key(None, "x").unwrap();
        let mut blob = encrypt(&key, b"hi").unwrap();
        blob[0] = 9;
        assert!(matches!(
            decrypt(&key, &blob),
            Err(CredentialCryptoError::UnsupportedVersion(9))
        ));
    }
}
