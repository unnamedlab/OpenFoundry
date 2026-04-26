# Restricted views and data controls

Data protection in OpenFoundry is not only about authentication. It also needs controlled projections of data for different audiences and contexts.

## Repository signals

The auth service includes explicit support for restricted views through:

- `handlers/restricted_views.rs`
- `models/restricted_view.rs`
- migration history that mentions restricted views and CBAC-style controls

These signals suggest that OpenFoundry is already moving toward a layered data-protection model rather than a simple binary allow/deny gate.

## Why this matters

Restricted views are especially useful for:

- regulated or marked datasets
- tenant-aware operational data
- partial exposure to external partners
- object-level and field-level semantics in ontology-driven apps
