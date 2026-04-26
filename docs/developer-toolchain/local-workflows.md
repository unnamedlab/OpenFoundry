# Local workflows

The local developer experience in OpenFoundry is intentionally centered around a small number of repeatable commands.

## Primary entry points

- `just` for workspace-wide tasks
- `cargo` for direct Rust crate work
- `pnpm` for the frontend
- `npm` for the isolated docs site
- Docker Compose for local infrastructure

## Core commands

The main contributor surface is defined in `justfile`.

Important flows include:

- `just build`
- `just test`
- `just lint`
- `just dev-stack`
- `just smoke`
- `just docs-build`
- `just ci-frontend`

## Why this matters

The toolchain is not only about convenience. It is also how the repo keeps backend, frontend, generated contracts, smoke scenarios, and docs synchronized in one monorepo.
