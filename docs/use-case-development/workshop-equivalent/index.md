# Workshop equivalent

OpenFoundry does not currently expose a product named Workshop, but `app-builder-service` and the `apps` route family strongly suggest an equivalent application composition surface.

## Repository signals

`app-builder-service` already exposes:

- app CRUD
- template-based app creation
- widget catalog access
- page creation and updates
- preview
- version listing
- publish
- slate package import and export

These routes are defined in `services/app-builder-service/src/main.rs`.

## Why this matters

This is the clearest bridge from capability primitives to user-facing operational applications.

## Section map

- [App composition lifecycle](/use-case-development/workshop-equivalent/app-composition-lifecycle)
- [Template, widget, and publish flow](/use-case-development/workshop-equivalent/template-widget-publish-flow)
- [OpenFoundry current vs target](/use-case-development/workshop-equivalent/current-vs-target)
