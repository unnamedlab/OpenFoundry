//! Round-trip tests for the Arrow ↔ provider type mapping.
//!
//! For every (provider, common Arrow type) pair, the test asserts:
//!   1. `provider_for(provider, arrow)` returns a non-empty label.
//!   2. Feeding that label back into `arrow_for(provider, label)`
//!      produces the same Arrow type.
//!
//! The non-representable types (BigQuery `GEOGRAPHY`, Snowflake
//! `VARIANT`, Databricks `INTERVAL`) are checked separately to make
//! sure they fall back to `Utf8` *with* a warning instead of silently
//! losing the column.

use virtual_table_service::domain::capability_matrix::SourceProvider;
use virtual_table_service::domain::type_mapping::{ArrowType, arrow_for, provider_for};

const COMMON_ARROW: &[ArrowType] = &[
    ArrowType::Boolean,
    ArrowType::Int64,
    ArrowType::Float64,
    ArrowType::Utf8,
    ArrowType::Binary,
    ArrowType::Date32,
    ArrowType::Timestamp,
    ArrowType::List,
    ArrowType::Struct,
];

fn round_trip_skipping(provider: SourceProvider, skip: &[ArrowType]) {
    for arrow in COMMON_ARROW.iter().copied() {
        if skip.contains(&arrow) {
            continue;
        }
        let label = provider_for(provider, arrow);
        assert!(
            !label.is_empty(),
            "{:?} × {:?} produced empty provider label",
            provider,
            arrow
        );
        let back = arrow_for(provider, label);
        assert_eq!(
            back.arrow, arrow,
            "{:?}: {:?} did not round-trip ('{}' decoded as {:?})",
            provider, arrow, label, back.arrow
        );
    }
}

fn round_trip(provider: SourceProvider) {
    round_trip_skipping(provider, &[]);
}

#[test]
fn snowflake_round_trip_skips_int64_because_number_widens_to_decimal() {
    // See `domain::type_mapping::tests::snowflake_int64_widens_to_decimal_through_number`
    // for the documented lossy round-trip — Snowflake's `NUMBER` is
    // both integer and decimal.
    round_trip_skipping(SourceProvider::Snowflake, &[ArrowType::Int64]);
}

#[test]
fn bigquery_round_trip() {
    round_trip(SourceProvider::BigQuery);
}

#[test]
fn databricks_round_trip() {
    round_trip(SourceProvider::Databricks);
}

#[test]
fn s3_round_trip() {
    round_trip(SourceProvider::AmazonS3);
}

#[test]
fn bigquery_geography_falls_back_to_utf8_with_warning() {
    let m = arrow_for(SourceProvider::BigQuery, "GEOGRAPHY");
    assert_eq!(m.arrow, ArrowType::Utf8);
    assert!(m.warning.is_some());
}

#[test]
fn snowflake_variant_falls_back_to_utf8_with_warning() {
    let m = arrow_for(SourceProvider::Snowflake, "VARIANT");
    assert_eq!(m.arrow, ArrowType::Utf8);
    assert!(m.warning.is_some());
}

#[test]
fn databricks_interval_falls_back_to_utf8_with_warning() {
    let m = arrow_for(SourceProvider::Databricks, "INTERVAL");
    assert_eq!(m.arrow, ArrowType::Utf8);
    assert!(m.warning.is_some());
}

#[test]
fn parquet_int64_with_logical_annotation_is_int64() {
    // The leading-token parser must strip Parquet's logical-type suffix.
    let m = arrow_for(SourceProvider::AmazonS3, "INT64 (TIMESTAMP_MILLIS)");
    assert_eq!(m.arrow, ArrowType::Int64);
}
