//! Operator state backend.
//!
//! The legacy [`simulate_state_store`] helper synthesises a
//! [`StateStoreSnapshot`] for the runtime preview surface; it is kept
//! here so the existing handlers and tests do not need to change.
//!
//! On top of that we expose the [`StateBackend`] trait that the
//! checkpoint subsystem (Bloque C) uses to materialise the in-flight
//! operator state to a durable medium. Two implementations ship in
//! tree:
//!   * [`InMemoryStateBackend`] — always available, used by tests and
//!     by deployments that do not need crash recovery.
//!   * [`RocksDbStateBackend`] — gated behind the `rocksdb-state`
//!     Cargo feature so the default build does not need to compile the
//!     `librocksdb-sys` C++ tree.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use chrono::Utc;
use uuid::Uuid;

use crate::models::{sink::StateStoreSnapshot, topology::TopologyDefinition};

pub fn simulate_state_store(topology: &TopologyDefinition, key_count: i32) -> StateStoreSnapshot {
    StateStoreSnapshot {
        backend: topology.state_backend.clone(),
        namespace: topology.name.to_lowercase().replace(' ', "-"),
        key_count,
        disk_usage_mb: 96 + key_count / 4,
        checkpoint_count: 7,
        last_checkpoint_at: Utc::now(),
    }
}

/// Errors surfaced by any [`StateBackend`] implementation.
#[derive(Debug, thiserror::Error)]
pub enum StateBackendError {
    #[error("state backend unavailable: {0}")]
    Unavailable(String),
    #[error("state backend i/o error: {0}")]
    Io(String),
    #[error("state backend serialisation error: {0}")]
    Serialize(String),
}

/// A keyed durable map scoped to a single (topology, key) pair.
#[async_trait::async_trait]
pub trait StateBackend: Send + Sync + std::fmt::Debug {
    fn id(&self) -> &'static str;

    async fn put(
        &self,
        topology_id: Uuid,
        key: &str,
        value: Vec<u8>,
    ) -> Result<(), StateBackendError>;

    /// Return an opaque payload that captures every key under
    /// `topology_id`. Format is implementation-defined.
    async fn snapshot(&self, topology_id: Uuid) -> Result<Vec<u8>, StateBackendError>;

    /// Replace the key/value space for `topology_id` with the contents
    /// of `payload` (previously produced by [`Self::snapshot`]).
    async fn restore(&self, topology_id: Uuid, payload: &[u8]) -> Result<usize, StateBackendError>;
}

pub type SharedStateBackend = Arc<dyn StateBackend>;

#[derive(Debug, Default)]
pub struct InMemoryStateBackend {
    inner: Mutex<HashMap<Uuid, HashMap<String, Vec<u8>>>>,
}

impl InMemoryStateBackend {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn shared() -> SharedStateBackend {
        Arc::new(Self::default())
    }
}

#[async_trait::async_trait]
impl StateBackend for InMemoryStateBackend {
    fn id(&self) -> &'static str {
        "in-memory"
    }

    async fn put(
        &self,
        topology_id: Uuid,
        key: &str,
        value: Vec<u8>,
    ) -> Result<(), StateBackendError> {
        let mut guard = self
            .inner
            .lock()
            .map_err(|e| StateBackendError::Unavailable(e.to_string()))?;
        guard
            .entry(topology_id)
            .or_default()
            .insert(key.to_string(), value);
        Ok(())
    }

    async fn snapshot(&self, topology_id: Uuid) -> Result<Vec<u8>, StateBackendError> {
        let guard = self
            .inner
            .lock()
            .map_err(|e| StateBackendError::Unavailable(e.to_string()))?;
        let map = guard.get(&topology_id).cloned().unwrap_or_default();
        serde_json::to_vec(&map).map_err(|e| StateBackendError::Serialize(e.to_string()))
    }

    async fn restore(&self, topology_id: Uuid, payload: &[u8]) -> Result<usize, StateBackendError> {
        let map: HashMap<String, Vec<u8>> = if payload.is_empty() {
            HashMap::new()
        } else {
            serde_json::from_slice(payload)
                .map_err(|e| StateBackendError::Serialize(e.to_string()))?
        };
        let count = map.len();
        let mut guard = self
            .inner
            .lock()
            .map_err(|e| StateBackendError::Unavailable(e.to_string()))?;
        guard.insert(topology_id, map);
        Ok(count)
    }
}

#[cfg(feature = "rocksdb-state")]
mod rocks {
    use super::{StateBackend, StateBackendError};
    use rocksdb::{DB, IteratorMode, Options};
    use std::collections::HashMap;
    use std::path::PathBuf;
    use std::sync::Arc;
    use uuid::Uuid;

    /// Filesystem-backed state backend using RocksDB. We keep all
    /// topologies in a single column family and prefix the keys with
    /// `<topology_id>/` to keep operations cheap.
    #[derive(Debug)]
    pub struct RocksDbStateBackend {
        db: Arc<DB>,
        path: PathBuf,
    }

    impl RocksDbStateBackend {
        pub fn open(path: impl Into<PathBuf>) -> Result<Self, StateBackendError> {
            let path = path.into();
            std::fs::create_dir_all(&path).map_err(|e| StateBackendError::Io(e.to_string()))?;
            let mut opts = Options::default();
            opts.create_if_missing(true);
            opts.set_compression_type(rocksdb::DBCompressionType::Lz4);
            let db = DB::open(&opts, &path).map_err(|e| StateBackendError::Io(e.to_string()))?;
            Ok(Self {
                db: Arc::new(db),
                path,
            })
        }

        pub fn path(&self) -> &PathBuf {
            &self.path
        }

        fn full_key(topology_id: Uuid, key: &str) -> Vec<u8> {
            format!("{}/{}", topology_id.simple(), key).into_bytes()
        }

        fn prefix_for(topology_id: Uuid) -> Vec<u8> {
            format!("{}/", topology_id.simple()).into_bytes()
        }
    }

    #[async_trait::async_trait]
    impl StateBackend for RocksDbStateBackend {
        fn id(&self) -> &'static str {
            "rocksdb"
        }

        async fn put(
            &self,
            topology_id: Uuid,
            key: &str,
            value: Vec<u8>,
        ) -> Result<(), StateBackendError> {
            let db = self.db.clone();
            let key = Self::full_key(topology_id, key);
            tokio::task::spawn_blocking(move || db.put(&key, &value))
                .await
                .map_err(|e| StateBackendError::Io(e.to_string()))?
                .map_err(|e| StateBackendError::Io(e.to_string()))
        }

        async fn snapshot(&self, topology_id: Uuid) -> Result<Vec<u8>, StateBackendError> {
            let db = self.db.clone();
            let prefix = Self::prefix_for(topology_id);
            tokio::task::spawn_blocking(move || -> Result<Vec<u8>, String> {
                let mut map: HashMap<String, Vec<u8>> = HashMap::new();
                let iter = db.iterator(IteratorMode::From(&prefix, rocksdb::Direction::Forward));
                for item in iter {
                    let (k, v) = item.map_err(|e| e.to_string())?;
                    if !k.starts_with(&prefix) {
                        break;
                    }
                    let suffix = String::from_utf8_lossy(&k[prefix.len()..]).into_owned();
                    map.insert(suffix, v.to_vec());
                }
                serde_json::to_vec(&map).map_err(|e| e.to_string())
            })
            .await
            .map_err(|e| StateBackendError::Io(e.to_string()))?
            .map_err(StateBackendError::Serialize)
        }

        async fn restore(
            &self,
            topology_id: Uuid,
            payload: &[u8],
        ) -> Result<usize, StateBackendError> {
            let map: HashMap<String, Vec<u8>> = if payload.is_empty() {
                HashMap::new()
            } else {
                serde_json::from_slice(payload)
                    .map_err(|e| StateBackendError::Serialize(e.to_string()))?
            };
            let db = self.db.clone();
            let prefix = Self::prefix_for(topology_id);
            let count = map.len();
            tokio::task::spawn_blocking(move || -> Result<(), String> {
                let iter = db.iterator(IteratorMode::From(&prefix, rocksdb::Direction::Forward));
                let mut to_delete: Vec<Vec<u8>> = Vec::new();
                for item in iter {
                    let (k, _) = item.map_err(|e| e.to_string())?;
                    if !k.starts_with(&prefix) {
                        break;
                    }
                    to_delete.push(k.to_vec());
                }
                for k in to_delete {
                    db.delete(&k).map_err(|e| e.to_string())?;
                }
                for (suffix, v) in map {
                    let mut full = prefix.clone();
                    full.extend_from_slice(suffix.as_bytes());
                    db.put(&full, &v).map_err(|e| e.to_string())?;
                }
                Ok(())
            })
            .await
            .map_err(|e| StateBackendError::Io(e.to_string()))?
            .map_err(StateBackendError::Io)?;
            Ok(count)
        }
    }
}

#[cfg(feature = "rocksdb-state")]
pub use rocks::RocksDbStateBackend;

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn in_memory_backend_round_trips_snapshot() {
        let backend = InMemoryStateBackend::new();
        let topology_id = Uuid::now_v7();
        backend
            .put(topology_id, "k1", b"v1".to_vec())
            .await
            .unwrap();
        backend
            .put(topology_id, "k2", b"v2".to_vec())
            .await
            .unwrap();
        let snap = backend.snapshot(topology_id).await.unwrap();
        assert!(!snap.is_empty());

        let other = InMemoryStateBackend::new();
        let restored = other.restore(topology_id, &snap).await.unwrap();
        assert_eq!(restored, 2);
        let snap2 = other.snapshot(topology_id).await.unwrap();
        // HashMap iteration order is unspecified, so compare semantic
        // content rather than the raw bytes.
        let m1: std::collections::HashMap<String, Vec<u8>> = serde_json::from_slice(&snap).unwrap();
        let m2: std::collections::HashMap<String, Vec<u8>> =
            serde_json::from_slice(&snap2).unwrap();
        assert_eq!(m1, m2);
    }
}
