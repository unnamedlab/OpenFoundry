# Plugin SDK

The plugin SDK is the contract layer for generated or packaged platform extensions.

## Repository signals

`libs/plugin-sdk` already defines:

- `PluginKind` with `connector`, `transform`, and `widget`
- runtime metadata
- manifests
- connector, transform, and widget plugin traits
- scaffold helpers for `Cargo.toml`, manifest JSON, and starter `lib.rs`

## Why this matters

This is a strong signal that OpenFoundry is designed to support extension as a platform feature, not only as internal source code changes.

## Design direction

The SDK can become the shared foundation for:

- marketplace packages
- connector templates
- transform authoring
- widget packaging
