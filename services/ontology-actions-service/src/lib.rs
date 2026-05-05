//! `ontology-actions-service` library surface.
//!
//! The service binary in `src/main.rs` consumes this crate to build its Axum
//! router. Tests under `tests/` import [`build_router`] to drive the same
//! routes against a `tower::ServiceExt::oneshot` client without binding a
//! TCP socket.
//!
//! ## Consolidation owner (S8.1)
//!
//! Per [`docs/architecture/service-consolidation-map.md`](../../../docs/architecture/service-consolidation-map.md)
//! and [ADR-0030](../../../docs/architecture/adr/ADR-0030-service-consolidation-30-targets.md),
//! this crate is the runtime owner of four merged ontology bounded
//! contexts that share the Cassandra ontology keyspace and the
//! `actions_log` writeback path:
//!
//! * **actions** — `/api/v1/ontology/actions/*` and inline-edit (this
//!   crate's original surface).
//! * **funnel** — `/api/v1/ontology/funnel/*` and
//!   `/api/v1/ontology/storage/insights` (absorbed from
//!   `ontology-funnel-service`).
//! * **functions** — `/api/v1/ontology/functions/*` plus the
//!   [`media_functions`] runtime trait (absorbed from
//!   `ontology-functions-service`).
//! * **rules** — `/api/v1/ontology/rules/*`,
//!   `/api/v1/ontology/types/{id}/rules`,
//!   `/api/v1/ontology/objects/{id}/rule-runs` (absorbed from
//!   `ontology-security-service`).

pub mod config;
pub mod media_functions;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router, middleware,
    routing::{delete, get, patch, post},
};
use ontology_kernel::{
    AppState,
    handlers::{
        actions::{
            create_action_type, create_action_what_if_branch, delete_action_type,
            delete_action_what_if_branch, execute_action, execute_action_batch,
            execute_inline_edit, execute_inline_edit_batch, get_action_metrics, get_action_type,
            list_action_types, list_action_what_if_branches, list_applicable_actions,
            update_action_type, upload_action_attachment, validate_action,
        },
        functions, funnel, rules, storage,
    },
};

/// Mounts the full HTTP surface of `ontology-actions-service` under
/// `/api/v1`. The `auth_middleware::layer::auth_layer` is applied to the
/// nested router so every Action / funnel / function / rule endpoint
/// requires a valid Bearer token — `GET /health` and `GET /metrics`
/// stay open and are added by the binary in `main.rs`.
pub fn build_router(state: AppState) -> Router {
    let actions = Router::new()
        .route(
            "/actions",
            get(list_action_types).post(create_action_type),
        )
        .route(
            "/actions/{id}",
            get(get_action_type)
                .put(update_action_type)
                .delete(delete_action_type),
        )
        .route("/actions/{id}/validate", post(validate_action))
        .route("/actions/{id}/execute", post(execute_action))
        .route("/actions/{id}/metrics", get(get_action_metrics))
        .route(
            "/actions/{id}/execute-batch",
            post(execute_action_batch),
        )
        .route(
            "/actions/{id}/what-if",
            get(list_action_what_if_branches).post(create_action_what_if_branch),
        )
        .route(
            "/actions/{id}/what-if/{branch_id}",
            delete(delete_action_what_if_branch),
        )
        .route(
            "/types/{type_id}/properties/{property_id}/objects/{obj_id}/inline-edit",
            post(execute_inline_edit),
        )
        // TASK L — Bulk inline-edit endpoint. Validates each entry
        // independently and rejects duplicates targeting the same object.
        .route(
            "/types/{type_id}/inline-edit-batch",
            post(execute_inline_edit_batch),
        )
        // TASK N — Applicable actions helper. Filters actions attached to
        // an object type by selection kind (single vs bulk).
        .route(
            "/types/{type_id}/applicable-actions",
            get(list_applicable_actions),
        )
        // TASK P — Attachment upload endpoint. Returns an opaque
        // attachment_rid that callers thread through as the value of
        // `attachment` or `media_reference` action parameters.
        .route("/actions/uploads", post(upload_action_attachment));

    // S8.1 — funnel + storage routes absorbed from `ontology-funnel-service`.
    let funnel_routes = Router::new()
        .route("/funnel/health", get(funnel::get_funnel_health))
        .route("/storage/insights", get(storage::get_storage_insights))
        .route(
            "/funnel/sources",
            get(funnel::list_funnel_sources).post(funnel::create_funnel_source),
        )
        .route(
            "/funnel/sources/{id}",
            get(funnel::get_funnel_source)
                .patch(funnel::update_funnel_source)
                .delete(funnel::delete_funnel_source),
        )
        .route(
            "/funnel/sources/{id}/health",
            get(funnel::get_funnel_source_health),
        )
        .route("/funnel/sources/{id}/run", post(funnel::trigger_funnel_run))
        .route("/funnel/sources/{id}/runs", get(funnel::list_funnel_runs))
        .route(
            "/funnel/sources/{source_id}/runs/{run_id}",
            get(funnel::get_funnel_run),
        );

    // S8.1 — function-package routes absorbed from `ontology-functions-service`.
    let functions_routes = Router::new()
        .route(
            "/functions",
            get(functions::list_function_packages).post(functions::create_function_package),
        )
        .route(
            "/functions/authoring-surface",
            get(functions::get_function_authoring_surface),
        )
        .route(
            "/functions/{id}",
            get(functions::get_function_package)
                .patch(functions::update_function_package)
                .delete(functions::delete_function_package),
        )
        .route(
            "/functions/{id}/validate",
            post(functions::validate_function_package),
        )
        .route(
            "/functions/{id}/simulate",
            post(functions::simulate_function_package),
        )
        .route(
            "/functions/{id}/runs",
            get(functions::list_function_package_runs),
        )
        .route(
            "/functions/{id}/metrics",
            get(functions::get_function_package_metrics),
        );

    // S8.1 — rule-engine routes absorbed from `ontology-security-service`.
    let rules_routes = Router::new()
        .route("/rules", get(rules::list_rules).post(rules::create_rule))
        .route("/rules/insights", get(rules::get_machinery_insights))
        .route("/rules/machinery/queue", get(rules::get_machinery_queue))
        .route(
            "/rules/machinery/queue/{id}",
            patch(rules::update_machinery_queue_item),
        )
        .route(
            "/rules/{id}",
            get(rules::get_rule)
                .patch(rules::update_rule)
                .delete(rules::delete_rule),
        )
        .route("/rules/{id}/simulate", post(rules::simulate_rule))
        .route("/rules/{id}/apply", post(rules::apply_rule))
        .route(
            "/types/{type_id}/rules",
            get(rules::list_rules_for_object_type),
        )
        .route(
            "/objects/{obj_id}/rule-runs",
            get(rules::list_object_rule_runs),
        );

    let protected = actions
        .merge(funnel_routes)
        .merge(functions_routes)
        .merge(rules_routes);

    Router::new()
        .nest("/api/v1/ontology", protected)
        .layer(middleware::from_fn_with_state(
            state.jwt_config.clone(),
            auth_middleware::layer::auth_layer,
        ))
        .with_state(state)
}

/// Convenience constructor used by both the binary and integration tests.
pub fn jwt_config_from_secret(secret: &str) -> JwtConfig {
    JwtConfig::new(secret).with_env_defaults()
}
