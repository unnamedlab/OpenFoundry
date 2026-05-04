//! Cargo entry-point for the `integration` test binary.
//!
//! Cargo discovers test binaries from `tests/*.rs` (one binary per file)
//! and `tests/<name>/main.rs` (one binary per directory). We use the
//! second form here so the H3 closure suite can grow into multiple
//! sibling files (`full_lifecycle.rs`, future `…_journey.rs`, …) while
//! sharing one Postgres testcontainer per binary boot — which is much
//! cheaper than one container per file.

#[path = "../common/mod.rs"]
mod common;

mod full_lifecycle;
