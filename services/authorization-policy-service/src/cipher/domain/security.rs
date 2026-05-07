use base64::{Engine as _, engine::general_purpose::URL_SAFE_NO_PAD};
use hmac::{Hmac, Mac};
use sha2::{Digest, Sha256};

pub fn hash_content(content: &str, salt: Option<&str>) -> String {
    let mut hasher = Sha256::new();
    if let Some(salt) = salt {
        hasher.update(salt.as_bytes());
    }
    hasher.update(content.as_bytes());
    URL_SAFE_NO_PAD.encode(hasher.finalize())
}

pub fn sign_content(content: &str, key_material: &str) -> String {
    let mut mac = Hmac::<Sha256>::new_from_slice(key_material.as_bytes())
        .expect("hmac accepts arbitrary key sizes");
    mac.update(content.as_bytes());
    URL_SAFE_NO_PAD.encode(mac.finalize().into_bytes())
}

pub fn verify_signature(content: &str, key_material: &str, signature: &str) -> bool {
    sign_content(content, key_material) == signature
}

pub fn encrypt_content(content: &str, key_material: &str) -> String {
    let key = derive_key_stream(key_material, content.len());
    let ciphertext = content
        .as_bytes()
        .iter()
        .zip(key.iter())
        .map(|(left, right)| left ^ right)
        .collect::<Vec<u8>>();
    URL_SAFE_NO_PAD.encode(ciphertext)
}

pub fn decrypt_content(ciphertext: &str, key_material: &str) -> Result<String, String> {
    let bytes = URL_SAFE_NO_PAD
        .decode(ciphertext)
        .map_err(|cause| format!("invalid ciphertext: {cause}"))?;
    let key = derive_key_stream(key_material, bytes.len());
    let plaintext = bytes
        .iter()
        .zip(key.iter())
        .map(|(left, right)| left ^ right)
        .collect::<Vec<u8>>();
    String::from_utf8(plaintext).map_err(|cause| format!("invalid plaintext payload: {cause}"))
}

fn derive_key_stream(key_material: &str, len: usize) -> Vec<u8> {
    let mut stream = Vec::with_capacity(len);
    let mut counter = 0_u64;
    while stream.len() < len {
        let mut hasher = Sha256::new();
        hasher.update(key_material.as_bytes());
        hasher.update(counter.to_le_bytes());
        stream.extend_from_slice(&hasher.finalize());
        counter += 1;
    }
    stream.truncate(len);
    stream
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn content_signatures_roundtrip() {
        let signature = sign_content("payload", "secret");
        assert!(verify_signature("payload", "secret", &signature));
        assert!(!verify_signature("payload", "wrong", &signature));
    }

    #[test]
    fn content_encryption_roundtrip() {
        let ciphertext = encrypt_content("payload", "secret");
        let plaintext = decrypt_content(&ciphertext, "secret").expect("decrypt");
        assert_eq!(plaintext, "payload");
    }
}
