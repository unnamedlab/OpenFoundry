//! Declarative routing table that maps topic patterns to messaging backends.

pub mod config;
pub mod table;

pub use config::{ConfigError, RouterConfig};
pub use table::{BackendId, CompiledRoute, ResolvedRoute, RouteTable};
