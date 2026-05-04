use crate::backend::{StorageError, StorageResult};

/// Configuration for generating pre-signed URLs (for direct upload/download).
#[derive(Debug, Clone)]
pub struct SignedUrlConfig {
    pub expiry_secs: u64,
}

impl Default for SignedUrlConfig {
    fn default() -> Self {
        Self { expiry_secs: 3600 }
    }
}

pub fn presigned_upload_url(
    bucket: &str,
    path: &str,
    _config: &SignedUrlConfig,
) -> StorageResult<String> {
    unsupported_presign("upload", bucket, path)
}

pub fn presigned_download_url(
    bucket: &str,
    path: &str,
    _config: &SignedUrlConfig,
) -> StorageResult<String> {
    unsupported_presign("download", bucket, path)
}

fn unsupported_presign(operation: &str, bucket: &str, path: &str) -> StorageResult<String> {
    Err(StorageError::Unsupported(format!(
        "pre-signed {operation} URLs are not available for bucket '{bucket}' path '{path}'"
    )))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::backend::StorageError;

    #[test]
    fn presign_upload_is_explicitly_unsupported() {
        let err = presigned_upload_url("media", "tenant/item", &SignedUrlConfig::default())
            .expect_err("unsupported backend must not return an empty URL");
        assert!(matches!(err, StorageError::Unsupported(_)));
    }

    #[test]
    fn presign_download_is_explicitly_unsupported() {
        let err = presigned_download_url("media", "tenant/item", &SignedUrlConfig::default())
            .expect_err("unsupported backend must not return an empty URL");
        assert!(matches!(err, StorageError::Unsupported(_)));
    }
}
