# Project init

Project scaffolding is the fastest way to make the developer platform feel coherent.

## Repository signals

`of-cli` already exposes `project init` with template selection for:

- connector
- transform
- widget

The command group is defined in `tools/of-cli/src/main.rs`, and the generated templates are supported by helper logic in `libs/plugin-sdk`.

## Why this matters

A good `project init` flow does three things:

- reduces setup errors
- encodes platform conventions
- shortens the path from idea to runnable artifact
