//! `ontology-functions-service` library surface.
//!
//! The binary in `main.rs` consumes this crate so integration tests
//! and downstream services can reach the function-runtime types
//! without spinning up the HTTP server.
//!
//! H6 adds the `media_functions` module — the five built-in
//! transformations the Foundry "Functions on objects → Media" doc
//! exposes (`read_raw`, `ocr`, `extract_text`, `transcribe_audio`,
//! `read_metadata`). Each function delegates to a
//! [`media_functions::MediaFunctionRuntime`] implementation; the
//! binary wires it to `media-transform-runtime-service`, while
//! tests inject a `MockMediaRuntime`.

pub mod media_functions;
