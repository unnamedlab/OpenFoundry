# Marketplace and packaging

Packaging is where internal tooling becomes distributable product capability.

## Repository signals

The marketplace domain has been migrated to its successor services:

- **`marketplace-catalog-service`** owns: catalog, listings, discovery, registry, validation, activation, dependency handling, install and publish flows, devops fleet and rollout models
- **`federation-product-exchange-service`** owns: install activation flows and dependency resolution
- **`application-curation-service`** owns: reviews and ratings
- **`developer-console-service`** owns: devops, fleet management, and CI/promotion gate integrations

Domain code is under `services/marketplace-catalog-service/src/domain/*` and `services/marketplace-catalog-service/src/handlers/*`.

## Why this matters

This subtree is the natural home for documentation about:

- publishing assets
- promotion gates
- installation and activation
- dependency resolution
- fleet and rollout models
