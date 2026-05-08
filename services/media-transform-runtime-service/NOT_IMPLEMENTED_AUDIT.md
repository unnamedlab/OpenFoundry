# NotImplemented parity audit

This audit covers every Go catalog row whose handler status is
`not_implemented` and compares it with the Rust
`services/media-transform-runtime-service/src/catalog.rs` source. The
Rust runtime short-circuits `HandlerStatus::NotImplemented` before
handler dispatch and returns the stable `NOT_IMPLEMENTED` transform
envelope with `compute_seconds: 0`, no output payload, and the catalog
reason verbatim.

`geo_tile` and `render_sheet` intentionally left this audit when their
Go adapters were wired: `geo_tile` now uses `libs/geospatial-tiles` for
XYZ coordinate validation, tile paths, and descriptors before rendering
PNG raster tiles; `render_sheet` is an explicit in-process CSV/JSON
adapter because notebook-runtime-service has no spreadsheet-render HTTP
route to call today.

| Key | Rust behavior | Go parity decision |
| --- | --- | --- |
| `embedding` | `HandlerStatus::NotImplemented` with reason `Image embeddings depend on libs/ai-kernel which is not yet wired.` | Keep as `not_implemented`; no AI-kernel image embedding handler was wired in Rust. |
| `transcription` | `HandlerStatus::NotImplemented` with reason `Transcription depends on libs/ai-kernel (Whisper / VLM) which is not yet wired.` | Keep as `not_implemented`; no Whisper/VLM sidecar was wired in Rust. |
| `layout_aware_v2` | `HandlerStatus::NotImplemented` with reason `Layout-aware extraction depends on libs/ai-kernel which is not yet wired.` | Keep as `not_implemented`; no layout-aware AI-kernel handler was wired in Rust. |
| `vlm_extract` | `HandlerStatus::NotImplemented` with reason `VLM extraction depends on libs/ai-kernel which is not yet wired.` | Keep as `not_implemented`; no VLM extraction handler was wired in Rust. |

Regression coverage lives in:

- `internal/catalog/catalog_test.go`, which pins the remaining audited
  catalog subset and canonical Rust reasons.
- `internal/server/server_test.go`, which asserts that each audited key
  returns the Rust-compatible `NOT_IMPLEMENTED` transform envelope.
- `internal/handlers/image_ops_test.go` and `internal/server/server_test.go`,
  which exercise the native `geo_tile` and `render_sheet` adapters.
