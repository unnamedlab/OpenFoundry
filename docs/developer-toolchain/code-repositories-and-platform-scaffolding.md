# Code repositories and platform scaffolding

OpenFoundry’s developer platform is more than build scripts. The repo already contains signals of a productized internal developer platform.

## Repository signals

- `services/code-repo-service`
- `services/application-composition-service`
- `services/marketplace-catalog-service`
- `libs/plugin-sdk`
- `tools/of-cli` project scaffolding commands

## CLI scaffolding

The CLI currently exposes project initialization and platform-oriented flows through:

- `of project init`
- deploy plan rendering
- script rendering
- plugin and scaffold-aware code paths

Those entry points are defined in `tools/of-cli/src/main.rs`.

## Why this matters

This area is the beginning of a real platform builder story:

- creating new packages and templates
- managing code artifacts as platform resources
- connecting build assets to application-composition and marketplace capabilities
