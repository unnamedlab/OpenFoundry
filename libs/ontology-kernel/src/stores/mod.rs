//! Storage trait bag for the ontology kernel.
//!
//! All persistence in `ontology-kernel` is being migrated from raw `sqlx`
//! call sites to the `storage-abstraction` traits so that the same handlers
//! can be wired against:
//!
//! * `CassandraObjectStore` / `CassandraLinkStore` / `CassandraActionLogStore`
//!   (the production target — see ADR-0020),
//! * the legacy `Postgres*` adapters in [`pg`] (only behind the `legacy-pg`
//!   feature, used while handlers are migrated one service at a time per
//!   `docs/architecture/migration-plan-cassandra-foundry-parity.md` §S1.4–S1.7),
//! * `mockall`-generated mocks for unit tests (see [`mock`]).
//!
//! The kernel's `AppState` carries a single [`Stores`] handle so handlers stay
//! infrastructure-agnostic.
//!
//! Migration status: this module ships in S1.2 as the substrate. A pilot
//! handler (`handlers::links::create_link` / `delete_link`) routes through
//! [`crate::domain::composition`] using these traits as the blueprint for
//! S1.4–S1.7.

use std::sync::Arc;

use storage_abstraction::repositories::{
    ActionLogStore, DefinitionStore, LinkStore, ObjectSetMaterializationStore, ObjectStore,
    ReadModelStore, SearchBackedObjectSetMaterializationStore, SearchBackend,
};

#[cfg(feature = "legacy-pg")]
pub mod pg;

#[cfg(feature = "mocks")]
pub mod mock;

/// Bundle of the storage trait objects the kernel handlers depend on.
///
/// Declarative definitions may still be backed by PostgreSQL (`pg-schemas`),
/// but they live behind [`DefinitionStore`] so live handlers no longer need to
/// know whether a row comes from Postgres, Cassandra, or an in-memory fake.
#[derive(Clone)]
pub struct Stores {
    pub objects: Arc<dyn ObjectStore>,
    pub links: Arc<dyn LinkStore>,
    pub actions: Arc<dyn ActionLogStore>,
    pub definitions: Arc<dyn DefinitionStore>,
    pub read_models: Arc<dyn ReadModelStore>,
    pub search: Arc<dyn SearchBackend>,
    pub object_set_materializations: Arc<dyn ObjectSetMaterializationStore>,
}

impl Stores {
    /// Build a [`Stores`] backed by the in-memory noop implementations
    /// shipped by `storage-abstraction`. Intended for unit tests and for
    /// smoke-testing handlers without spinning up infrastructure.
    pub fn in_memory() -> Self {
        use storage_abstraction::repositories::noop::*;
        let search = Arc::new(InMemorySearchBackend::default());
        Self {
            objects: Arc::new(InMemoryObjectStore::default()),
            links: Arc::new(InMemoryLinkStore::default()),
            actions: Arc::new(InMemoryActionLogStore::default()),
            definitions: Arc::new(InMemoryDefinitionStore::default()),
            read_models: Arc::new(InMemoryReadModelStore::default()),
            search: search.clone(),
            object_set_materializations: Arc::new(SearchBackedObjectSetMaterializationStore::new(
                search,
            )),
        }
    }
}
