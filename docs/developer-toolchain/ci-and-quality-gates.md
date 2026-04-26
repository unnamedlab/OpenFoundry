# CI and quality gates

OpenFoundry’s toolchain is backed by strong CI expectations rather than by convention alone.

## Main workflow layers

- Rust correctness, test, smoke, and dependency policy in `ci.yml`
- frontend lint, typecheck, unit, E2E, and build in `ci-frontend.yml`
- proto and generated artifact drift in `proto-check.yml`
- Helm, Terraform, SDK, docs, release, and container publication in specialized workflows

## What stands out

The Rust CI pipeline does more than compile:

- it provisions Postgres and Redis
- it creates separate databases for many services
- it launches a large local runtime mesh
- it runs multi-phase smoke scenarios through the built binaries

That behavior is defined in [.github/workflows/ci.yml](/Users/torrefacto/Documents/Repositorios/OpenFoundry/.github/workflows/ci.yml).

## Why this matters

These checks turn architecture assumptions into executable tests:

- database-per-service boundaries
- gateway-to-service wiring
- ontology and workflow readiness
- AI/ML and analytics critical paths
