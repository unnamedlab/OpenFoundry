#!/usr/bin/env python3
"""Scaffold the P59–P85 batch of services.

Each service is generated with a uniform skeleton:
- Cargo.toml
- Dockerfile (with allocated port)
- src/main.rs (AppState, JwtConfig, /health, protected CRUD on a primary resource)
- src/config.rs
- src/handlers.rs (list + create + get for the primary resource)
- src/models.rs (FromRow + request bodies for the primary resource)
- migrations/<ts>_<slug>_foundation.sql (primary table + secondary table)
"""
from __future__ import annotations

import os
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SERVICES = ROOT / "services"

# (slug, description, port, primary_route, primary_table, primary_columns, secondary_table, secondary_columns)
# primary_columns/secondary_columns: list of (name, sql_type, rust_type) — first column "id UUID" implicit.
SERVICES_SPEC = [
    (
        "ontology-timeseries-analytics-service",
        "Dashboards and analytics combining ontology entities with time-series workloads",
        50132,
        "/api/v1/ontology-timeseries/dashboards",
        "ontology_timeseries_dashboards",
        "definition JSONB NOT NULL DEFAULT '{}'::jsonb, owner_id UUID NOT NULL",
        "ontology_timeseries_queries",
        "dashboard_id UUID NOT NULL, payload JSONB NOT NULL DEFAULT '{}'::jsonb",
    ),
    (
        "sql-bi-gateway-service",
        "SQL and BI gateway: execute, explain, saved queries and BI tool compatibility",
        50133,
        "/api/v1/sql-bi/queries",
        "sql_bi_saved_queries",
        "name TEXT NOT NULL, sql TEXT NOT NULL, owner_id UUID NOT NULL",
        "sql_bi_executions",
        "query_id UUID NOT NULL, status TEXT NOT NULL DEFAULT 'queued', result JSONB NOT NULL DEFAULT '{}'::jsonb",
    ),
    (
        "notebook-runtime-service",
        "Interactive notebook runtime: cells, kernels, sessions and collaboration",
        50134,
        "/api/v1/notebook-runtime/notebooks",
        "notebook_runtime_notebooks",
        "title TEXT NOT NULL, owner_id UUID NOT NULL, config JSONB NOT NULL DEFAULT '{}'::jsonb",
        "notebook_runtime_sessions",
        "notebook_id UUID NOT NULL, kernel TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'idle'",
    ),
    (
        "spreadsheet-computation-service",
        "Collaborative spreadsheets: formulas, recalculation and writeback",
        50135,
        "/api/v1/spreadsheets/sheets",
        "spreadsheet_sheets",
        "name TEXT NOT NULL, owner_id UUID NOT NULL, schema JSONB NOT NULL DEFAULT '{}'::jsonb",
        "spreadsheet_recalcs",
        "sheet_id UUID NOT NULL, status TEXT NOT NULL DEFAULT 'queued', result JSONB NOT NULL DEFAULT '{}'::jsonb",
    ),
    (
        "analytical-logic-service",
        "Saved expressions, reusable analytical logic and visual function templates",
        50136,
        "/api/v1/analytical-logic/expressions",
        "analytical_expressions",
        "name TEXT NOT NULL, kind TEXT NOT NULL, definition JSONB NOT NULL DEFAULT '{}'::jsonb",
        "analytical_expression_versions",
        "expression_id UUID NOT NULL, version INT NOT NULL, body JSONB NOT NULL DEFAULT '{}'::jsonb",
    ),
    (
        "workflow-automation-service",
        "Continuous and scheduled business automations: definitions, runs and rules",
        50137,
        "/api/v1/workflow-automations/definitions",
        "workflow_automation_definitions",
        "name TEXT NOT NULL, trigger_kind TEXT NOT NULL, definition JSONB NOT NULL DEFAULT '{}'::jsonb",
        "workflow_automation_runs",
        "definition_id UUID NOT NULL, status TEXT NOT NULL DEFAULT 'queued', result JSONB NOT NULL DEFAULT '{}'::jsonb",
    ),
    (
        "automation-operations-service",
        "Automations operational control plane: queues, retries, dependencies and per-object execution",
        50138,
        "/api/v1/automation-ops/queues",
        "automation_queues",
        "name TEXT NOT NULL, scope JSONB NOT NULL DEFAULT '{}'::jsonb, status TEXT NOT NULL DEFAULT 'active'",
        "automation_queue_runs",
        "queue_id UUID NOT NULL, run_ref TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'pending'",
    ),
    (
        "workflow-trace-service",
        "Workflow run history, traces, logs and provenance across functions, actions, models and apps",
        50139,
        "/api/v1/workflow-traces/runs",
        "workflow_trace_runs",
        "workflow_id UUID NOT NULL, started_at TIMESTAMPTZ NOT NULL DEFAULT now(), status TEXT NOT NULL DEFAULT 'running'",
        "workflow_trace_events",
        "run_id UUID NOT NULL, kind TEXT NOT NULL, payload JSONB NOT NULL DEFAULT '{}'::jsonb",
    ),
    (
        "application-composition-service",
        "Composition runtime: views, state, bindings, page layout and event orchestration",
        50140,
        "/api/v1/app-composition/views",
        "composition_views",
        "name TEXT NOT NULL, layout JSONB NOT NULL DEFAULT '{}'::jsonb, owner_id UUID NOT NULL",
        "composition_bindings",
        "view_id UUID NOT NULL, target TEXT NOT NULL, expression JSONB NOT NULL DEFAULT '{}'::jsonb",
    ),
    (
        "scenario-simulation-service",
        "What-if branches, immutable forks and scenario runs over actions and models",
        50141,
        "/api/v1/scenarios/simulations",
        "scenario_simulations",
        "name TEXT NOT NULL, base_state JSONB NOT NULL DEFAULT '{}'::jsonb, status TEXT NOT NULL DEFAULT 'draft'",
        "scenario_runs",
        "simulation_id UUID NOT NULL, status TEXT NOT NULL DEFAULT 'queued', result JSONB NOT NULL DEFAULT '{}'::jsonb",
    ),
    (
        "solution-design-service",
        "Architecture knowledge base: diagrams, patterns and platform references",
        50142,
        "/api/v1/solution-design/diagrams",
        "solution_diagrams",
        "title TEXT NOT NULL, kind TEXT NOT NULL, body JSONB NOT NULL DEFAULT '{}'::jsonb",
        "solution_references",
        "diagram_id UUID NOT NULL, ref_kind TEXT NOT NULL, ref_value TEXT NOT NULL",
    ),
    (
        "developer-console-service",
        "Developer console for application admin, scopes, subdomains, releases and config",
        50143,
        "/api/v1/developer-console/applications",
        "developer_applications",
        "name TEXT NOT NULL, subdomain TEXT NOT NULL UNIQUE, scopes JSONB NOT NULL DEFAULT '[]'::jsonb",
        "developer_releases",
        "application_id UUID NOT NULL, version TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'draft'",
    ),
    (
        "sdk-generation-service",
        "SDK and OpenAPI contract generation, publication and versioning",
        50144,
        "/api/v1/sdk-generation/jobs",
        "sdk_generation_jobs",
        "language TEXT NOT NULL, ontology_version TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'queued'",
        "sdk_generation_publications",
        "job_id UUID NOT NULL, location TEXT NOT NULL, version TEXT NOT NULL",
    ),
    (
        "managed-workspace-service",
        "Managed dev workspaces: profiles, dataset aliases, builder branches and context resolution",
        50145,
        "/api/v1/managed-workspaces/workspaces",
        "managed_workspaces",
        "name TEXT NOT NULL, profile JSONB NOT NULL DEFAULT '{}'::jsonb, owner_id UUID NOT NULL",
        "managed_workspace_aliases",
        "workspace_id UUID NOT NULL, alias TEXT NOT NULL, target TEXT NOT NULL",
    ),
    (
        "custom-endpoints-service",
        "Custom endpoint set publishing, versioning and HTTP remap to actions and functions",
        50146,
        "/api/v1/custom-endpoints/sets",
        "custom_endpoint_sets",
        "name TEXT NOT NULL, version TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'draft'",
        "custom_endpoints",
        "set_id UUID NOT NULL, path TEXT NOT NULL, method TEXT NOT NULL, target JSONB NOT NULL DEFAULT '{}'::jsonb",
    ),
    (
        "mcp-orchestration-service",
        "MCP exposure of internal and ontological tools for agents, apps and external consumers",
        50147,
        "/api/v1/mcp/servers",
        "mcp_servers",
        "name TEXT NOT NULL, transport TEXT NOT NULL, config JSONB NOT NULL DEFAULT '{}'::jsonb",
        "mcp_tools",
        "server_id UUID NOT NULL, name TEXT NOT NULL, schema JSONB NOT NULL DEFAULT '{}'::jsonb",
    ),
    (
        "compute-modules-control-plane-service",
        "Compute modules control plane: lifecycle, deployment, replicas, diagnostics and config",
        50148,
        "/api/v1/compute-modules/modules",
        "compute_modules",
        "name TEXT NOT NULL, image TEXT NOT NULL, replicas INT NOT NULL DEFAULT 1, config JSONB NOT NULL DEFAULT '{}'::jsonb",
        "compute_module_deployments",
        "module_id UUID NOT NULL, status TEXT NOT NULL DEFAULT 'pending', diagnostics JSONB NOT NULL DEFAULT '{}'::jsonb",
    ),
    (
        "compute-modules-runtime-service",
        "Compute modules runtime: execution under platform identity with scaling and metrics",
        50149,
        "/api/v1/compute-modules/runs",
        "compute_module_runs",
        "module_id UUID NOT NULL, status TEXT NOT NULL DEFAULT 'queued', started_at TIMESTAMPTZ",
        "compute_module_metrics",
        "run_id UUID NOT NULL, kind TEXT NOT NULL, value DOUBLE PRECISION NOT NULL",
    ),
    (
        "monitoring-rules-service",
        "Monitoring rules engine: monitors, scopes, severities and subscribers at scale",
        50150,
        "/api/v1/monitoring/rules",
        "monitoring_rules",
        "name TEXT NOT NULL, severity TEXT NOT NULL DEFAULT 'info', scope JSONB NOT NULL DEFAULT '{}'::jsonb, definition JSONB NOT NULL DEFAULT '{}'::jsonb",
        "monitoring_subscribers",
        "rule_id UUID NOT NULL, channel TEXT NOT NULL, target TEXT NOT NULL",
    ),
    (
        "execution-observability-service",
        "Run history, log search, distributed tracing and execution debug",
        50152,
        "/api/v1/execution-observability/runs",
        "execution_runs",
        "source TEXT NOT NULL, run_ref TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'running', metadata JSONB NOT NULL DEFAULT '{}'::jsonb",
        "execution_logs",
        "run_id UUID NOT NULL, level TEXT NOT NULL, message TEXT NOT NULL, ts TIMESTAMPTZ NOT NULL DEFAULT now()",
    ),
    (
        "telemetry-governance-service",
        "Telemetry permissions, log/metric/event export and governance policies",
        50153,
        "/api/v1/telemetry-governance/exports",
        "telemetry_exports",
        "name TEXT NOT NULL, sink TEXT NOT NULL, config JSONB NOT NULL DEFAULT '{}'::jsonb, status TEXT NOT NULL DEFAULT 'active'",
        "telemetry_policies",
        "export_id UUID NOT NULL, kind TEXT NOT NULL, body JSONB NOT NULL DEFAULT '{}'::jsonb",
    ),
    (
        "code-security-scanning-service",
        "Static security analysis, code smells and CI quality policy enforcement",
        50154,
        "/api/v1/code-security/scans",
        "code_security_scans",
        "repository TEXT NOT NULL, ref TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'queued', summary JSONB NOT NULL DEFAULT '{}'::jsonb",
        "code_security_findings",
        "scan_id UUID NOT NULL, severity TEXT NOT NULL, rule TEXT NOT NULL, location TEXT NOT NULL, message TEXT NOT NULL",
    ),
]


CARGO_TEMPLATE = """[package]
name = "{slug}"
description = "{desc}"
version.workspace = true
edition.workspace = true
rust-version.workspace = true
license.workspace = true

[dependencies]
core-models = {{ workspace = true }}
auth-middleware = {{ workspace = true }}
axum = {{ workspace = true }}
tokio = {{ workspace = true }}
sqlx = {{ workspace = true }}
serde = {{ workspace = true }}
serde_json = {{ workspace = true }}
tracing = {{ workspace = true }}
tracing-subscriber = {{ workspace = true }}
uuid = {{ workspace = true }}
chrono = {{ workspace = true }}
config = {{ workspace = true }}
reqwest = {{ workspace = true }}
"""

DOCKERFILE_TEMPLATE = """FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \\
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \\
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p {slug}

FROM debian:bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \\
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \\
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT={port}

COPY --from=builder /workspace/target/release/{slug} /usr/local/bin/{slug}

EXPOSE {port}
CMD ["/usr/local/bin/{slug}"]
"""

MAIN_TEMPLATE = """mod config;
mod handlers;
mod models;

use auth_middleware::jwt::JwtConfig;
use axum::{{Router, extract::FromRef, middleware, routing::get}};
use core_models::{{health::HealthStatus, observability}};
use sqlx::postgres::PgPoolOptions;

#[derive(Clone)]
pub struct AppState {{
    pub db: sqlx::PgPool,
    pub jwt_config: JwtConfig,
    pub http_client: reqwest::Client,
}}

impl FromRef<AppState> for JwtConfig {{
    fn from_ref(state: &AppState) -> Self {{ state.jwt_config.clone() }}
}}

#[tokio::main]
async fn main() {{
    observability::init_tracing("{slug}");
    let cfg = config::AppConfig::from_env().expect("failed to load config");
    let pool = PgPoolOptions::new()
        .max_connections(15)
        .connect(&cfg.database_url)
        .await
        .expect("failed to connect to database");
    sqlx::migrate!("./migrations").run(&pool).await.expect("failed to run migrations");

    let jwt_config = JwtConfig::new(&cfg.jwt_secret).with_env_defaults();
    let http_client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(60))
        .build()
        .expect("failed to build HTTP client");

    let state = AppState {{
        db: pool,
        jwt_config: jwt_config.clone(),
        http_client,
    }};

    let public = Router::new().route(
        "/health",
        get(|| async {{ axum::Json(HealthStatus::ok("{slug}")) }}),
    );

    let protected = Router::new()
        .route(
            "{primary_route}",
            get(handlers::list_items).post(handlers::create_item),
        )
        .route(
            "{primary_route}/{{id}}",
            get(handlers::get_item),
        )
        .route(
            "{primary_route}/{{id}}/secondary",
            get(handlers::list_secondary).post(handlers::create_secondary),
        )
        .layer(middleware::from_fn_with_state(jwt_config, auth_middleware::auth_layer));

    let app = Router::new().merge(public).merge(protected).with_state(state);
    let addr = format!("{{}}:{{}}", cfg.host, cfg.port);
    tracing::info!("starting {slug} on {{addr}}");
    let listener = tokio::net::TcpListener::bind(&addr).await.expect("failed to bind");
    axum::serve(listener, app).await.expect("server error");
}}
"""

CONFIG_TEMPLATE = """use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {{
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    pub database_url: String,
    pub jwt_secret: String,
}}

fn default_host() -> String {{ "0.0.0.0".to_string() }}
fn default_port() -> u16 {{ {port} }}

impl AppConfig {{
    pub fn from_env() -> Result<Self, config::ConfigError> {{
        config::Config::builder()
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }}
}}
"""

HANDLERS_TEMPLATE = """use axum::{{
    Json,
    extract::{{Path, State}},
    http::StatusCode,
    response::IntoResponse,
}};
use uuid::Uuid;

use crate::AppState;
use crate::models::{{
    CreatePrimaryRequest, CreateSecondaryRequest, PrimaryItem, SecondaryItem,
}};

pub async fn list_items(State(state): State<AppState>) -> impl IntoResponse {{
    match sqlx::query_as::<_, PrimaryItem>(
        "SELECT * FROM {primary_table} ORDER BY created_at DESC LIMIT 200",
    )
    .fetch_all(&state.db)
    .await
    {{
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }}
}}

pub async fn create_item(
    State(state): State<AppState>,
    Json(body): Json<CreatePrimaryRequest>,
) -> impl IntoResponse {{
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, PrimaryItem>(
        "INSERT INTO {primary_table} (id, payload) VALUES ($1, $2) RETURNING *",
    )
    .bind(id)
    .bind(&body.payload)
    .fetch_one(&state.db)
    .await
    {{
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }}
}}

pub async fn get_item(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {{
    match sqlx::query_as::<_, PrimaryItem>(
        "SELECT * FROM {primary_table} WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {{
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "not found").into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }}
}}

pub async fn list_secondary(
    State(state): State<AppState>,
    Path(parent_id): Path<Uuid>,
) -> impl IntoResponse {{
    match sqlx::query_as::<_, SecondaryItem>(
        "SELECT * FROM {secondary_table} WHERE parent_id = $1 ORDER BY created_at DESC LIMIT 200",
    )
    .bind(parent_id)
    .fetch_all(&state.db)
    .await
    {{
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }}
}}

pub async fn create_secondary(
    State(state): State<AppState>,
    Path(parent_id): Path<Uuid>,
    Json(body): Json<CreateSecondaryRequest>,
) -> impl IntoResponse {{
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, SecondaryItem>(
        "INSERT INTO {secondary_table} (id, parent_id, payload) VALUES ($1, $2, $3) RETURNING *",
    )
    .bind(id)
    .bind(parent_id)
    .bind(&body.payload)
    .fetch_one(&state.db)
    .await
    {{
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }}
}}
"""

MODELS_TEMPLATE = """use chrono::{{DateTime, Utc}};
use serde::{{Deserialize, Serialize}};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct PrimaryItem {{
    pub id: Uuid,
    pub payload: serde_json::Value,
    pub created_at: DateTime<Utc>,
}}

#[derive(Debug, Clone, Deserialize)]
pub struct CreatePrimaryRequest {{
    pub payload: serde_json::Value,
}}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct SecondaryItem {{
    pub id: Uuid,
    pub parent_id: Uuid,
    pub payload: serde_json::Value,
    pub created_at: DateTime<Utc>,
}}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateSecondaryRequest {{
    pub payload: serde_json::Value,
}}
"""

# Use uniform simplified migrations: payload + created_at on every table.
MIGRATION_TEMPLATE = """CREATE TABLE IF NOT EXISTS {primary_table} (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{{}}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_{primary_table}_created_at ON {primary_table}(created_at);

CREATE TABLE IF NOT EXISTS {secondary_table} (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES {primary_table}(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{{}}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_{secondary_table}_parent_id ON {secondary_table}(parent_id);
"""


def write(p: Path, content: str) -> None:
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(content)


def main() -> None:
    ts = "20260427070600"
    for i, (
        slug,
        desc,
        port,
        primary_route,
        primary_table,
        _primary_cols,
        secondary_table,
        _secondary_cols,
    ) in enumerate(SERVICES_SPEC):
        base = SERVICES / slug
        if base.exists():
            print(f"SKIP {slug} (exists)")
            continue
        ctx = dict(
            slug=slug,
            desc=desc,
            port=port,
            primary_route=primary_route,
            primary_table=primary_table,
            secondary_table=secondary_table,
        )
        write(base / "Cargo.toml", CARGO_TEMPLATE.format(**ctx))
        write(base / "Dockerfile", DOCKERFILE_TEMPLATE.format(**ctx))
        write(base / "src/main.rs", MAIN_TEMPLATE.format(**ctx))
        write(base / "src/config.rs", CONFIG_TEMPLATE.format(**ctx))
        write(base / "src/handlers.rs", HANDLERS_TEMPLATE.format(**ctx))
        write(base / "src/models.rs", MODELS_TEMPLATE.format(**ctx))
        mig_name = f"{ts[:-2]}{str(int(ts[-2:]) + i).zfill(2)}_{primary_table}_foundation.sql"
        # avoid overflow: just use ts + index suffix
        mig_name = f"{ts}_{i:02d}_{primary_table}_foundation.sql"
        write(base / "migrations" / mig_name, MIGRATION_TEMPLATE.format(**ctx))
        print(f"CREATED {slug} on port {port}")


if __name__ == "__main__":
    main()
