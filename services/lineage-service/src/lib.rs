//! `lineage-service` — substrate-only library.
//!
//! Surfaces a `kafka_to_iceberg` module that pins the topic and the
//! Iceberg target for the `lineage.events.v1` → `of.lineage.*`
//! pipeline. The actual writer lands in S5.2.

pub mod iceberg_schema;
pub mod kafka_to_iceberg;
pub mod query_router;
