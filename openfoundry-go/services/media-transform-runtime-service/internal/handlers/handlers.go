// Package handlers will host the native (pure-Go) image
// transformations. Foundation slice intentionally ships nothing here:
// the runtime returns 501 NOT_IMPLEMENTED with a stable reason for
// every entry until the image-handler slice lands.
//
// When the follow-up slice arrives, this file gets:
//   - decode/encode helpers backed by Go stdlib + golang.org/x/image
//     for webp/bmp/tiff round-trip parity with the Rust `image` crate.
//   - 6 handlers (thumbnail, resize, resize_within_bounding_box,
//     rotate, crop, grayscale) matching the Rust signatures.
//   - The `Dispatch(kind, mime, params, src) (HandlerOutput, error)`
//     entry-point the runtime calls.
package handlers
