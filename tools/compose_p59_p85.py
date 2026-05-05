#!/usr/bin/env python3
"""Generate docker-compose blocks for the P59-P85 batch."""

SERVICES = [
    ("ontology-timeseries-analytics-service", 50132, "ontology_timeseries_analytics", "OPENFOUNDRY_ONTOLOGY_TIMESERIES_ANALYTICS_HOST_PORT"),
    ("sql-bi-gateway-service", 50133, "sql_bi_gateway", "OPENFOUNDRY_SQL_BI_GATEWAY_HOST_PORT"),
    ("notebook-runtime-service", 50134, "notebook_runtime", "OPENFOUNDRY_NOTEBOOK_RUNTIME_HOST_PORT"),
    ("spreadsheet-computation-service", 50135, "spreadsheet_computation", "OPENFOUNDRY_SPREADSHEET_COMPUTATION_HOST_PORT"),
    # S8 / ADR-0030: `analytical-logic-service` retired — see
    # `tools/scaffold_p59_p85.py` and `libs/analytical-logic`.
    ("workflow-automation-service", 50137, "workflow_automation", "OPENFOUNDRY_WORKFLOW_AUTOMATION_HOST_PORT"),
    ("automation-operations-service", 50138, "automation_operations", "OPENFOUNDRY_AUTOMATION_OPERATIONS_HOST_PORT"),
    ("workflow-trace-service", 50139, "workflow_trace", "OPENFOUNDRY_WORKFLOW_TRACE_HOST_PORT"),
    ("application-composition-service", 50140, "application_composition", "OPENFOUNDRY_APPLICATION_COMPOSITION_HOST_PORT"),
    ("scenario-simulation-service", 50141, "scenario_simulation", "OPENFOUNDRY_SCENARIO_SIMULATION_HOST_PORT"),
    ("solution-design-service", 50142, "solution_design", "OPENFOUNDRY_SOLUTION_DESIGN_HOST_PORT"),
    ("developer-console-service", 50143, "developer_console", "OPENFOUNDRY_DEVELOPER_CONSOLE_HOST_PORT"),
    ("sdk-generation-service", 50144, "sdk_generation", "OPENFOUNDRY_SDK_GENERATION_HOST_PORT"),
    ("managed-workspace-service", 50145, "managed_workspace", "OPENFOUNDRY_MANAGED_WORKSPACE_HOST_PORT"),
    ("custom-endpoints-service", 50146, "custom_endpoints", "OPENFOUNDRY_CUSTOM_ENDPOINTS_HOST_PORT"),
    ("mcp-orchestration-service", 50147, "mcp_orchestration", "OPENFOUNDRY_MCP_ORCHESTRATION_HOST_PORT"),
    ("compute-modules-control-plane-service", 50148, "compute_modules_control_plane", "OPENFOUNDRY_COMPUTE_MODULES_CONTROL_PLANE_HOST_PORT"),
    ("compute-modules-runtime-service", 50149, "compute_modules_runtime", "OPENFOUNDRY_COMPUTE_MODULES_RUNTIME_HOST_PORT"),
    ("monitoring-rules-service", 50150, "monitoring_rules", "OPENFOUNDRY_MONITORING_RULES_HOST_PORT"),
    ("execution-observability-service", 50152, "execution_observability", "OPENFOUNDRY_EXECUTION_OBSERVABILITY_HOST_PORT"),
    ("telemetry-governance-service", 50153, "telemetry_governance", "OPENFOUNDRY_TELEMETRY_GOVERNANCE_HOST_PORT"),
    ("code-security-scanning-service", 50154, "code_security_scanning", "OPENFOUNDRY_CODE_SECURITY_SCANNING_HOST_PORT"),
]

BLOCK = """  {slug}:
    <<: *app-common
    build:
      context: ..
      dockerfile: services/{slug}/Dockerfile
    environment:
      HOST: 0.0.0.0
      PORT: {port}
      DATABASE_URL: postgres://openfoundry:openfoundry@postgres:5432/openfoundry_{db}
      JWT_SECRET: ${{OPENFOUNDRY_JWT_SECRET:-openfoundry-dev-secret}}
    depends_on:
      postgres:
        condition: service_healthy
    ports:
      - "${{{host_var}:-{port}}}:{port}"
"""

ENVS = []
DEPS = []
BLOCKS = []
for slug, port, db, host_var in SERVICES:
    BLOCKS.append(BLOCK.format(slug=slug, port=port, db=db, host_var=host_var))
    ENVS.append(f"      {slug.upper().replace('-', '_')}_URL: http://{slug}:{port}")
    DEPS.append(f"      {slug}:\n        condition: service_started")

print("=== BLOCKS ===")
print("\n".join(BLOCKS))
print("=== ENVS ===")
print("\n".join(ENVS))
print("=== DEPS ===")
print("\n".join(DEPS))
