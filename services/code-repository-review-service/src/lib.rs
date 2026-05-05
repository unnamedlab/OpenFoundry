//! `code-repository-review-service` — code repos, reviews, and the
//! cross-plane global-branching coordinator.
//!
//! Per ADR-0030 / the service-consolidation map, this crate is the
//! sole runtime owner of the code-repo, review and global-branching
//! domains, plus the code-security scan / finding stores absorbed
//! from the retired `code-security-scanning-service`.

pub mod code_security;
pub mod config;
pub mod global_branch;
pub mod router;

pub use config::AppConfig;
pub use router::{AppState, build_router};
