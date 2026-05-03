//! `pipeline-authoring-service` — minimal lib surface.
//!
//! The Axum + SQLx control plane lives in [`main.rs`]. This lib only
//! re-exposes the **pure-Rust** validators that integration tests
//! exercise without booting Postgres or HTTP — specifically the
//! Foundry-style media node + expression palettes added in P1.4.
//!
//! Modules listed here are *intentionally* a strict subset of what
//! `main.rs` reaches for; nothing else needs library visibility yet,
//! and broadening this lib would force AppState + handler types into
//! shared scope, which is more disruption than the current testing
//! needs warrant.
//!
//! Both modules are also reachable from the binary via
//! `crate::domain::{media_nodes, expressions}` — `lib.rs` re-roots the
//! same source files (`#[path]`) so there is no duplication of
//! authoritative code, only a second compilation pass under the lib's
//! crate root.

#[path = "domain/media_nodes.rs"]
pub mod media_nodes;

#[path = "domain/expressions.rs"]
pub mod expressions;

#[path = "domain/param.rs"]
pub mod param;

#[path = "domain/parameterized.rs"]
pub mod parameterized;
