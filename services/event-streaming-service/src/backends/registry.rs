//! Backend registry: holds the concrete [`Backend`] implementations keyed by
//! [`BackendId`].

use std::collections::HashMap;
use std::sync::Arc;

use crate::router::BackendId;

use super::Backend;

/// Lookup table from [`BackendId`] to a shared backend implementation.
#[derive(Clone, Default)]
pub struct BackendRegistry {
    inner: HashMap<BackendId, Arc<dyn Backend>>,
}

impl BackendRegistry {
    pub fn new() -> Self {
        Self {
            inner: HashMap::new(),
        }
    }

    pub fn insert(&mut self, backend: Arc<dyn Backend>) {
        self.inner.insert(backend.id(), backend);
    }

    pub fn with(mut self, backend: Arc<dyn Backend>) -> Self {
        self.insert(backend);
        self
    }

    pub fn get(&self, id: BackendId) -> Option<Arc<dyn Backend>> {
        self.inner.get(&id).cloned()
    }

    pub fn contains(&self, id: BackendId) -> bool {
        self.inner.contains_key(&id)
    }
}
