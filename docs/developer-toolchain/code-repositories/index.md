# Code repositories

Code repositories are a first-class platform capability in OpenFoundry, not only an external Git integration story.

## Repository signals

`code-repo-service` already exposes dedicated APIs for:

- repository listing and creation
- branches
- commits
- file listing and search
- diffs
- CI runs
- integrations
- merge requests and comments

The route surface is defined in `services/code-repo-service/src/main.rs`.

## Why this matters

This gives OpenFoundry a path toward embedded developer workflows inside the platform, especially when combined with app builder, marketplace, and project scaffolding.

## Section map

- [Repository lifecycle](/developer-toolchain/code-repositories/repository-lifecycle)
- [Developer platform flow](/developer-toolchain/code-repositories/developer-platform-flow)
- [OpenFoundry current vs target](/developer-toolchain/code-repositories/current-vs-target)
