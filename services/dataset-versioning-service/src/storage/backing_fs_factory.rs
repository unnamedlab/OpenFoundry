//! P3 — build the [`BackingFileSystem`] selected by the runtime config.
//!
//! Mirrors `storage::factory` for the writer side: maps the
//! [`crate::config::BackingFsConfig`] block (driver + endpoint /
//! bucket / region / base_directory / …) onto the right concrete
//! implementation from `storage_abstraction::backing_fs`.

use std::sync::Arc;

use storage_abstraction::backing_fs::{
    BackingFileSystem, HdfsBackingFs, LocalBackingFs, LocalBackingFsConfig, S3BackingFs,
    S3BackingFsConfig,
};
use storage_abstraction::backing_fs::hdfs::HdfsBackingFsConfig;
use storage_abstraction::local::LocalStorage;

use crate::config::BackingFsConfig;

#[derive(Debug, thiserror::Error)]
pub enum BackingFsFactoryError {
    #[error("unknown backing_fs.driver `{0}` (expected local | s3 | hdfs)")]
    UnknownDriver(String),
    #[error("backing_fs.driver=s3 requires `bucket`, `region`, `access_key`, `secret_key`")]
    MissingS3Settings,
    #[error("backing_fs.driver=hdfs requires `hdfs_namenode`")]
    MissingHdfsSettings,
    #[error("local storage init failed: {0}")]
    LocalInit(String),
    #[error("backing fs init failed: {0}")]
    Init(String),
}

/// Build the [`BackingFileSystem`] chosen by the operator. Returned as
/// an `Arc<dyn ...>` so it slots into [`crate::AppState`] alongside
/// the existing writer / storage handles.
pub fn build_backing_fs(
    cfg: &BackingFsConfig,
) -> Result<Arc<dyn BackingFileSystem>, BackingFsFactoryError> {
    match cfg.driver.trim().to_ascii_lowercase().as_str() {
        "local" => build_local(cfg),
        "s3" => build_s3(cfg),
        "hdfs" => build_hdfs(cfg),
        other => Err(BackingFsFactoryError::UnknownDriver(other.to_string())),
    }
}

fn build_local(
    cfg: &BackingFsConfig,
) -> Result<Arc<dyn BackingFileSystem>, BackingFsFactoryError> {
    // Make sure the local root exists; LocalFileSystem otherwise refuses
    // to bind. In production this directory is created by the
    // installer / Helm chart, so we only mkdir best-effort.
    std::fs::create_dir_all(&cfg.local_root).ok();
    let backend = LocalStorage::new(&cfg.local_root)
        .map_err(|e| BackingFsFactoryError::LocalInit(e.to_string()))?;
    let fs = LocalBackingFs::new(
        Arc::new(backend),
        LocalBackingFsConfig {
            fs_id: "local".into(),
            base_directory: cfg.base_directory.clone(),
            presign_secret: cfg.presign_secret.clone(),
            public_origin: cfg.public_origin.clone(),
        },
    )
    .map_err(|e| BackingFsFactoryError::Init(e.to_string()))?;
    Ok(arc_dyn(fs))
}

fn build_s3(
    cfg: &BackingFsConfig,
) -> Result<Arc<dyn BackingFileSystem>, BackingFsFactoryError> {
    if cfg.bucket.is_empty()
        || cfg.region.is_empty()
        || cfg.access_key.is_empty()
        || cfg.secret_key.is_empty()
    {
        return Err(BackingFsFactoryError::MissingS3Settings);
    }
    let fs = S3BackingFs::new(S3BackingFsConfig {
        bucket: cfg.bucket.clone(),
        region: cfg.region.clone(),
        base_directory: cfg.base_directory.clone(),
        endpoint: cfg.endpoint.clone(),
        access_key: cfg.access_key.clone(),
        secret_key: cfg.secret_key.clone(),
    })
    .map_err(|e| BackingFsFactoryError::Init(e.to_string()))?;
    Ok(arc_dyn(fs))
}

fn build_hdfs(
    cfg: &BackingFsConfig,
) -> Result<Arc<dyn BackingFileSystem>, BackingFsFactoryError> {
    if cfg.hdfs_namenode.is_empty() {
        return Err(BackingFsFactoryError::MissingHdfsSettings);
    }
    let fs = HdfsBackingFs::new(HdfsBackingFsConfig {
        namenode: cfg.hdfs_namenode.clone(),
        base_directory: cfg.base_directory.clone(),
        user: cfg.hdfs_user.clone(),
    });
    Ok(arc_dyn(fs))
}

fn arc_dyn<T: BackingFileSystem + 'static>(fs: T) -> Arc<dyn BackingFileSystem> {
    Arc::new(fs)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn cfg(driver: &str) -> BackingFsConfig {
        let mut c = BackingFsConfig::default();
        c.driver = driver.into();
        c
    }

    fn assert_err<E: std::fmt::Debug, T>(
        result: Result<T, E>,
        check: impl Fn(&E) -> bool,
    ) -> E {
        match result {
            Ok(_) => panic!("expected error, got Ok"),
            Err(e) => {
                assert!(check(&e), "unexpected error variant: {e:?}");
                e
            }
        }
    }

    #[test]
    fn unknown_driver_is_rejected() {
        assert_err(build_backing_fs(&cfg("nope")), |e| {
            matches!(e, BackingFsFactoryError::UnknownDriver(_))
        });
    }

    #[test]
    fn s3_without_credentials_is_rejected() {
        assert_err(build_backing_fs(&cfg("s3")), |e| {
            matches!(e, BackingFsFactoryError::MissingS3Settings)
        });
    }

    #[test]
    fn hdfs_without_namenode_is_rejected() {
        assert_err(build_backing_fs(&cfg("hdfs")), |e| {
            matches!(e, BackingFsFactoryError::MissingHdfsSettings)
        });
    }

    #[test]
    fn local_default_returns_a_handle() {
        let mut c = cfg("local");
        // tempdir-rooted local backend so the test doesn't fight
        // /var/lib permissions on CI runners.
        let dir = tempfile::tempdir().unwrap();
        c.local_root = dir.path().to_string_lossy().into_owned();
        c.base_directory = "foundry/datasets".into();
        let fs = build_backing_fs(&c).expect("local builds");
        assert_eq!(fs.fs_id(), "local");
        assert_eq!(fs.base_directory(), "foundry/datasets");
    }
}
