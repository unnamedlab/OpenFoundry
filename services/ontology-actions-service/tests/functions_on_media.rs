//! H6 — integration coverage for the five Foundry "Functions on
//! objects → Media" entry points (`read_raw`, `ocr`, `extract_text`,
//! `transcribe_audio`, `read_metadata`).
//!
//! Each test pins:
//!   * the call recorded against the runtime (so handlers can't drop
//!     a function call silently),
//!   * the parameter shape (the `MediaItemHandle` reaches the runtime
//!     verbatim, including `branch` + `schema` hints), and
//!   * the response surfaced to the caller (round-trips structured
//!     data — transcripts, metadata — without re-encoding).

use bytes::Bytes;
use ontology_actions_service::media_functions::{
    MediaFunctionError, MediaItemHandle, MockMediaRuntime, TranscriptSegment, Transcription,
    extract_text, ocr, read_metadata, read_raw, transcribe_audio,
};
use serde_json::json;

fn handle(item: &str) -> MediaItemHandle {
    let mut h = MediaItemHandle::new("ri.foundry.main.media_set.fixtures", item);
    h.branch = Some("main".into());
    h.schema = Some("DOCUMENT".into());
    h
}

#[tokio::test]
async fn read_raw_round_trips_scripted_bytes_and_records_the_call() {
    let runtime = MockMediaRuntime::new();
    runtime.put_raw("doc-1", Bytes::from_static(b"%PDF-1.4 raw bytes"));

    let item = handle("doc-1");
    let bytes = read_raw(&runtime, &item)
        .await
        .expect("read_raw must succeed");
    assert_eq!(bytes, Bytes::from_static(b"%PDF-1.4 raw bytes"));

    let log = runtime.calls();
    assert_eq!(log.len(), 1);
    assert_eq!(log[0].0, "read_raw");
    assert_eq!(
        log[0].1, item,
        "MediaItemHandle must reach the runtime verbatim"
    );
}

#[tokio::test]
async fn ocr_returns_scripted_text() {
    let runtime = MockMediaRuntime::new();
    runtime.put_ocr("photo-1", "Aircraft tail number AF-101");
    let text = ocr(&runtime, &handle("photo-1")).await.unwrap();
    assert_eq!(text, "Aircraft tail number AF-101");
}

#[tokio::test]
async fn extract_text_separate_from_ocr_path() {
    let runtime = MockMediaRuntime::new();
    runtime.put_text("doc-1", "Heading\nBody paragraph");
    // Scripting `text` but NOT `ocr` proves that `extract_text` does
    // not inadvertently fall back to the OCR script.
    let extracted = extract_text(&runtime, &handle("doc-1")).await.unwrap();
    assert_eq!(extracted, "Heading\nBody paragraph");
    assert!(
        ocr(&runtime, &handle("doc-1")).await.is_err(),
        "ocr must remain unscripted — paths must stay independent"
    );
}

#[tokio::test]
async fn transcribe_audio_round_trips_segments() {
    let runtime = MockMediaRuntime::new();
    runtime.put_transcript(
        "audio-1",
        Transcription {
            text: "Welcome to the briefing.".into(),
            segments: vec![
                TranscriptSegment {
                    start: 0.0,
                    end: 1.5,
                    text: "Welcome".into(),
                },
                TranscriptSegment {
                    start: 1.5,
                    end: 3.2,
                    text: "to the briefing.".into(),
                },
            ],
        },
    );
    let t = transcribe_audio(&runtime, &handle("audio-1"))
        .await
        .unwrap();
    assert_eq!(t.text, "Welcome to the briefing.");
    assert_eq!(t.segments.len(), 2);
    assert_eq!(t.segments[0].text, "Welcome");
    assert!((t.segments[1].end - 3.2).abs() < f64::EPSILON);
}

#[tokio::test]
async fn read_metadata_returns_a_json_blob() {
    let runtime = MockMediaRuntime::new();
    runtime.put_metadata(
        "doc-1",
        json!({
            "mime_type": "application/pdf",
            "size_bytes": 4_096,
            "sha256": "abc123",
            "pages": 12
        }),
    );
    let meta = read_metadata(&runtime, &handle("doc-1")).await.unwrap();
    assert_eq!(meta["mime_type"], "application/pdf");
    assert_eq!(meta["pages"], 12);
}

#[tokio::test]
async fn missing_item_surfaces_not_found() {
    let runtime = MockMediaRuntime::new();
    // No put_raw call — the runtime has nothing for `ghost`.
    let err = read_raw(&runtime, &handle("ghost")).await.unwrap_err();
    assert!(matches!(err, MediaFunctionError::NotFound(_)));
}

/// Sanity: every public entry point hits its own runtime method.
/// A bug like `extract_text` calling into `ocr` would be caught here.
#[tokio::test]
async fn each_function_dispatches_to_its_own_runtime_method() {
    let runtime = MockMediaRuntime::new();
    runtime
        .put_raw("a", Bytes::from_static(b""))
        .put_ocr("a", "")
        .put_text("a", "")
        .put_transcript(
            "a",
            Transcription {
                text: String::new(),
                segments: vec![],
            },
        )
        .put_metadata("a", json!({}));

    let _ = read_raw(&runtime, &handle("a")).await.unwrap();
    let _ = ocr(&runtime, &handle("a")).await.unwrap();
    let _ = extract_text(&runtime, &handle("a")).await.unwrap();
    let _ = transcribe_audio(&runtime, &handle("a")).await.unwrap();
    let _ = read_metadata(&runtime, &handle("a")).await.unwrap();

    let kinds: Vec<String> = runtime.calls().iter().map(|c| c.0.clone()).collect();
    assert_eq!(
        kinds,
        vec![
            "read_raw",
            "ocr",
            "extract_text",
            "transcribe_audio",
            "read_metadata",
        ]
    );
}
