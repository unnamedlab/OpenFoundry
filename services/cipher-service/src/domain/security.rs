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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn content_signatures_roundtrip() {
        let signature = sign_content("payload", "secret");
        assert!(verify_signature("payload", "secret", &signature));
        assert!(!verify_signature("payload", "wrong", &signature));
    }
}
