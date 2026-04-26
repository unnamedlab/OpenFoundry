# Ontology Manager

Ontology manager is the control-plane area for maintaining and evolving semantic definitions.

## Core concerns

- navigation and ownership
- change management
- review and restore flows
- usage visibility
- import and export of ontology definitions
- cleanup and migration support

## OpenFoundry mapping

This capability would likely sit across:

- `services/ontology-service`
- `services/auth-service`
- `services/audit-service`
- `apps/web/src/routes/ontology`

## Why it matters

As ontology scope grows, the platform needs a dedicated management surface rather than forcing semantic governance into raw config or direct backend edits.
