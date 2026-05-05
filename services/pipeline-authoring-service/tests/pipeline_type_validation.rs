//! FASE 1 — Pipeline-type coherence validator coverage.
//!
//! Exercises every rule in `domain/pipeline_type.rs::validate_pipeline_type_coherence`.
//!
//! Ref: docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Workflows/Building pipelines/Types of pipelines.md
//!      (Ref: Types of pipelines.screenshot.png)

use pipeline_authoring_service::pipeline_type::{
    self, ExternalConfig, IncrementalConfig, PipelineType, StreamingConfig,
    validate_pipeline_type_coherence,
};

#[test]
fn pipeline_type_round_trips() {
    for kind in [
        PipelineType::Batch,
        PipelineType::Faster,
        PipelineType::Incremental,
        PipelineType::Streaming,
        PipelineType::External,
    ] {
        assert_eq!(
            pipeline_type::PipelineType::parse(kind.as_str()).unwrap(),
            kind
        );
    }
}

#[test]
fn pipeline_type_parse_unknown_returns_none() {
    assert!(pipeline_type::PipelineType::parse("HOLOGRAM").is_none());
}

#[test]
fn batch_with_no_extras_is_coherent() {
    assert!(validate_pipeline_type_coherence(PipelineType::Batch, None, None, None).is_empty());
    assert!(validate_pipeline_type_coherence(PipelineType::Faster, None, None, None).is_empty());
}

#[test]
fn streaming_without_input_stream_is_rejected() {
    let errs = validate_pipeline_type_coherence(PipelineType::Streaming, None, None, None);
    assert!(
        errs.iter().any(|e| e.contains("input_stream_id")),
        "expected input_stream_id error, got {errs:?}"
    );
}

#[test]
fn streaming_with_empty_input_stream_is_rejected() {
    let cfg = StreamingConfig {
        input_stream_id: Some("   ".to_string()),
        ..Default::default()
    };
    let errs =
        validate_pipeline_type_coherence(PipelineType::Streaming, None, None, Some(&cfg));
    assert!(
        errs.iter().any(|e| e.contains("input_stream_id")),
        "blank input_stream_id must be rejected, got {errs:?}"
    );
}

#[test]
fn streaming_with_input_stream_is_coherent() {
    let cfg = StreamingConfig {
        input_stream_id: Some("018f-stream-uuid".to_string()),
        parallelism: 2,
        ..Default::default()
    };
    let errs =
        validate_pipeline_type_coherence(PipelineType::Streaming, None, None, Some(&cfg));
    assert!(errs.is_empty(), "expected coherent streaming, got {errs:?}");
}

#[test]
fn streaming_must_not_carry_external_config() {
    let stream = StreamingConfig {
        input_stream_id: Some("018f".to_string()),
        ..Default::default()
    };
    let ext = ExternalConfig {
        source_system: "databricks".to_string(),
        ..Default::default()
    };
    let errs = validate_pipeline_type_coherence(
        PipelineType::Streaming,
        Some(&ext),
        None,
        Some(&stream),
    );
    assert!(
        errs.iter().any(|e| e.contains("external")),
        "expected external-config rejection, got {errs:?}"
    );
}

#[test]
fn external_without_source_system_is_rejected() {
    let errs = validate_pipeline_type_coherence(PipelineType::External, None, None, None);
    assert!(
        errs.iter().any(|e| e.contains("source_system")),
        "expected source_system error, got {errs:?}"
    );

    let blank = ExternalConfig {
        source_system: "   ".to_string(),
        ..Default::default()
    };
    let errs =
        validate_pipeline_type_coherence(PipelineType::External, Some(&blank), None, None);
    assert!(
        errs.iter().any(|e| e.contains("source_system")),
        "blank source_system must be rejected, got {errs:?}"
    );
}

#[test]
fn external_with_source_system_is_coherent() {
    let ext = ExternalConfig {
        source_system: "snowflake".to_string(),
        compute_profile_id: Some("warehouse-xs".to_string()),
        ..Default::default()
    };
    let errs = validate_pipeline_type_coherence(PipelineType::External, Some(&ext), None, None);
    assert!(errs.is_empty(), "expected coherent external, got {errs:?}");
}

#[test]
fn external_must_not_carry_streaming_config() {
    let ext = ExternalConfig {
        source_system: "databricks".to_string(),
        ..Default::default()
    };
    let stream = StreamingConfig {
        input_stream_id: Some("018f".to_string()),
        ..Default::default()
    };
    let errs = validate_pipeline_type_coherence(
        PipelineType::External,
        Some(&ext),
        None,
        Some(&stream),
    );
    assert!(
        errs.iter().any(|e| e.contains("streaming")),
        "expected streaming-config rejection, got {errs:?}"
    );
}

#[test]
fn incremental_allowed_transaction_types_validates_tokens() {
    let bad = IncrementalConfig {
        allowed_transaction_types: "APPEND, FOO".to_string(),
        ..Default::default()
    };
    let errs =
        validate_pipeline_type_coherence(PipelineType::Incremental, None, Some(&bad), None);
    assert!(
        errs.iter().any(|e| e.contains("FOO")),
        "expected FOO rejection, got {errs:?}"
    );

    let good = IncrementalConfig {
        allowed_transaction_types: "APPEND,UPDATE".to_string(),
        replay_on_deploy: true,
        watermark_columns: vec!["event_ts".into()],
    };
    let errs =
        validate_pipeline_type_coherence(PipelineType::Incremental, None, Some(&good), None);
    assert!(errs.is_empty(), "expected ok, got {errs:?}");
}

#[test]
fn incremental_with_empty_allowed_types_is_coherent_default() {
    let cfg = IncrementalConfig::default();
    let errs =
        validate_pipeline_type_coherence(PipelineType::Incremental, None, Some(&cfg), None);
    assert!(
        errs.is_empty(),
        "empty allowed_transaction_types should default-fall-through, got {errs:?}"
    );
}

#[test]
fn incremental_must_not_carry_external_config() {
    let ext = ExternalConfig {
        source_system: "databricks".to_string(),
        ..Default::default()
    };
    let errs =
        validate_pipeline_type_coherence(PipelineType::Incremental, Some(&ext), None, None);
    assert!(
        errs.iter().any(|e| e.contains("external")),
        "expected external rejection, got {errs:?}"
    );
}

#[test]
fn batch_must_not_carry_external_or_streaming() {
    let ext = ExternalConfig {
        source_system: "databricks".to_string(),
        ..Default::default()
    };
    let stream = StreamingConfig {
        input_stream_id: Some("018f".to_string()),
        ..Default::default()
    };
    let errs = validate_pipeline_type_coherence(
        PipelineType::Batch,
        Some(&ext),
        None,
        Some(&stream),
    );
    assert!(errs.iter().any(|e| e.contains("external")), "{errs:?}");
    assert!(errs.iter().any(|e| e.contains("streaming")), "{errs:?}");
}
