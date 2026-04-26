# App composition lifecycle

The `app-builder-service` already suggests a lifecycle-oriented application composition model.

## Current lifecycle

1. create an app
2. create from template when appropriate
3. inspect the widget catalog
4. add or update pages
5. preview the app
6. inspect versions
7. publish the app

## Repository signals

These operations are routed through `services/app-builder-service/src/handlers/apps.rs`, `pages.rs`, `preview.rs`, `publish.rs`, and `widgets.rs`.
