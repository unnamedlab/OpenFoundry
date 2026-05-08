// Package handlers ports the Rust `libs/ontology-kernel/src/handlers/`
// module tree. Each subpackage owns one bounded context (storage,
// links, actions, funnel, functions, rules, …) and depends only on
// the kernel's `domain` and `models` packages plus the shared
// `storage-abstraction` interfaces.
//
// Migration order matches `docs/architecture/migration-plan-cassandra-foundry-parity.md`
// §S1.4–S1.7. As of this iteration the package ships:
//
//   - storage: `GetStorageInsights` (1:1 of `handlers/storage.rs`)
//   - links:   `CollectLinkInstancesForType` (only the helper the
//     storage handler reaches into; full link CRUD lands together
//     with the `composition.rs` port).
//
// The remaining handlers (actions, bindings, bulk, functions,
// funnel, interfaces, object_sets, objects, projects, properties,
// rules, search, shared_properties, types) are tracked as separate
// phases; until they land the consumer service mounts the URL grid
// against the partial set declared here.
package handlers
