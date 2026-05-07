#!/usr/bin/env python3
"""Generate docker-compose blocks for the P59-P85 batch."""

SERVICES = [
    # `ontology-timeseries-analytics-service` (was 50132) merged → `ontology-exploratory-analysis-service` per ADR-0030 (S8 / B20).
    ("sql-bi-gateway-service", 50133, "sql_bi_gateway", "OPENFOUNDRY_SQL_BI_GATEWAY_HOST_PORT"),
    ("notebook-runtime-service", 50134, "notebook_runtime", "OPENFOUNDRY_NOTEBOOK_RUNTIME_HOST_PORT"),
    # `spreadsheet-computation-service` (was 50135) merged → `notebook-runtime-service` per ADR-0030 (S8).
    # S8 / ADR-0030: `analytical-logic-service` retired — see
    # `tools/scaffold_p59_p85.py` and `libs/analytical-logic`.
    ("workflow-automation-service", 50137, "workflow_automation", "OPENFOUNDRY_WORKFLOW_AUTOMATION_HOST_PORT"),
    ("automation-operations-service", 50138, "automation_operations", "OPENFOUNDRY_AUTOMATION_OPERATIONS_HOST_PORT"),
    # `workflow-trace-service` (was 50139) merged → `lineage-service` per ADR-0030 (S8).
    ("application-composition-service", 50140, "application_composition", "OPENFOUNDRY_APPLICATION_COMPOSITION_HOST_PORT"),
    # `scenario-simulation-service` (was 50141) merged → `ontology-exploratory-analysis-service` per ADR-0030 (S8 / B20).
    ("solution-design-service", 50142, "solution_design", "OPENFOUNDRY_SOLUTION_DESIGN_HOST_PORT"),
    # `developer-console-service` (was 50143) merged → `application-composition-service` per ADR-0030 (S8 / B19).
    ("sdk-generation-service", 50144, "sdk_generation", "OPENFOUNDRY_SDK_GENERATION_HOST_PORT"),
    # `managed-workspace-service` (was 50145) and `custom-endpoints-service` (was 50146)
    # merged → `application-composition-service` per ADR-0030 (S8 / B19).
    # `mcp-orchestration-service` (was 50147) merged → `ai-evaluation-service` per ADR-0030 (S8 / B18).
    ("compute-modules-control-plane-service", 50148, "compute_modules_control_plane", "OPENFOUNDRY_COMPUTE_MODULES_CONTROL_PLANE_HOST_PORT"),
    ("compute-modules-runtime-service", 50149, "compute_modules_runtime", "OPENFOUNDRY_COMPUTE_MODULES_RUNTIME_HOST_PORT"),
    # `monitoring-rules-service` (was 50150) and
    # `execution-observability-service` (was 50152) merged →
    # `telemetry-governance-service` per ADR-0030 (S8 / B22).
    ("telemetry-governance-service", 50153, "telemetry_governance", "OPENFOUNDRY_TELEMETRY_GOVERNANCE_HOST_PORT"),
    # `code-security-scanning-service` (was 50154) merged → `code-repository-review-service` per ADR-0030 (S8).
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
