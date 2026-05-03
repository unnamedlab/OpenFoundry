//! `connector-management-service` — minimal lib surface.
//!
//! The Axum control plane lives in [`main.rs`]. This lib re-exposes
//! only the **pure-Rust** building blocks that integration tests
//! exercise without booting Postgres or HTTP — currently the
//! Foundry-style media-set sync filter logic added in P1.4.
//!
//! Both modules are also reachable from the binary via
//! `crate::domain::media_set_sync`; `lib.rs` re-roots the same source
//! files (`#[path]`) so there is no source duplication, only a second
//! compilation pass under the lib's crate root.

#[path = "domain/media_set_sync.rs"]
pub mod media_set_sync;
