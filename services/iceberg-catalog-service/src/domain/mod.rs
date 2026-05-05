//! Domain types and business logic for the Iceberg REST Catalog.
//!
//! ## P1 substrate
//! * [`namespace`]   — namespace creation, listing, properties.
//! * [`table`]       — table creation, schema persistence, drop logic.
//! * [`snapshot`]    — append/overwrite/delete snapshot tracking.
//! * [`branch`]      — Iceberg branch / tag refs.
//! * [`metadata`]    — `metadata.json` builder + parser (Iceberg spec v2).
//! * [`token`]       — long-lived API tokens (SHA-256 fingerprints).
//!
//! ## P2 (Foundry transaction layer)
//! * [`foundry_transaction`] — all-or-nothing wrapper that batches
//!   multi-table commits onto the spec's `/iceberg/v1/transactions/commit`.
//! * [`snapshot_mapping`]    — Iceberg ↔ Foundry transaction-type taxonomy.
//! * [`branch_alias`]        — `master ↔ main` rewrite (per doc § "Default branches").
//! * [`schema_strict`]       — schema-strict-mode enforcement + diff.

pub mod branch;
pub mod branch_alias;
pub mod foundry_transaction;
pub mod markings;
pub mod metadata;
pub mod namespace;
pub mod schema_strict;
pub mod snapshot;
pub mod snapshot_mapping;
pub mod table;
pub mod token;
