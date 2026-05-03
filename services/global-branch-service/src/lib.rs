//! `global-branch-service` — cross-plane branch coordination.
//!
//! Foundry "Global Branching application" + "Branch taskbar":
//! a *global branch* names a workstream that spans datasets,
//! ontology, pipelines and code repos. Each plane still owns its
//! own `*_branches` table; this service stores the cross-plane label
//! and the resource-link map, and consumes
//! `foundry.branch.events.v1` to keep the link statuses fresh.
//!
//! ## Surface
//!
//!   * `POST   /v1/global-branches` — create a global branch.
//!   * `GET    /v1/global-branches/{id}` — summary.
//!   * `POST   /v1/global-branches/{id}/links` — link a local branch.
//!   * `GET    /v1/global-branches/{id}/resources` — link table with
//!     per-resource sync status (`in_sync | drifted | archived`).
//!   * `POST   /v1/global-branches/{id}/promote` — emit
//!     `global.branch.promote.requested.v1` for downstream planes.
//!
//! The legacy `code_repo_base/*` tree (Code Repos / merge requests)
//! is reachable via the existing `src/handlers.rs` / `src/domain.rs`
//! / `src/models.rs` files and is unchanged by P4.

pub mod config;
pub mod global;
pub mod router;

pub use config::AppConfig;
pub use router::{AppState, build_router};
