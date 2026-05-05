//! Pipeline Builder expression DSL.
//!
//! Mirrors the type-safe behaviour described in
//! `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Workflows/Building pipelines/Considerations Pipeline Builder and Code Repositories.md`
//! ("Type-safe functions: errors are flagged immediately instead of at
//! build time"). Pure-Rust, no IO — meant to be embedded in
//! `pipeline-authoring-service` and surfaced to the canvas through the
//! per-node validation status.
//!
//! Three modules:
//!
//! * [`types`] — the runtime type lattice (Boolean / Integer / Long /
//!   Double / Decimal / String / Date / Timestamp / Array / Struct /
//!   Geometry) plus promotion rules.
//! * [`parser`] — hand-rolled lexer + Pratt-style parser for the
//!   Pipeline Builder expression mini-language. No external dep.
//! * [`catalog`] — typed signatures for the nine canonical transforms
//!   (cast, title_case, clean_string, filter, join, union, group_by,
//!   window, pivot) and supporting scalar helpers.
//! * [`infer`] — visits an [`Expr`](parser::Expr) under a column
//!   environment and returns either the inferred [`PipelineType`] or a
//!   list of [`TypeError`]s.

pub mod catalog;
pub mod infer;
pub mod node_check;
pub mod parser;
pub mod types;

pub use infer::{ColumnEnv, TypeError, infer_expr};
pub use node_check::{
    NodeValidationError, NodeValidationReport, PipelineValidationReport, validate_nodes_json,
};
pub use parser::{Expr, Literal, ParseError, parse_expr};
pub use types::{PipelineType, can_promote, promote};
