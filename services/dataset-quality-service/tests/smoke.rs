//! T7.3 — service smoke test for `dataset-quality-service`.
//!
//! The service binary is currently a stub (`fn main() {}`) and has no
//! HTTP router yet. This smoke is intentionally `#[ignore]`d and is
//! kept as a contract-pinning placeholder so that, the moment a router
//! lands, the test starts asserting `/healthz` + `/metrics` (with the
//! `dataset_` prefix) without anyone needing to remember to add it.

#[test]
#[ignore = "dataset-quality-service does not yet expose an HTTP router; \
            replace this stub with a router-driven smoke test once \
            `build_router` exists."]
fn placeholder_until_router_exists() {
    // Once the service ships a `build_router(AppState) -> Router`,
    // mirror the catalog/versioning smoke tests:
    //
    //   * GET /healthz → 200
    //   * GET /metrics → 200, body contains a metric line starting
    //     with `dataset_`.
}
