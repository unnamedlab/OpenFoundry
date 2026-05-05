#!/usr/bin/env python3
"""
Wire envSecrets DATABASE_URL/DATABASE_READ_URL bindings into each umbrella's
values-dev.yaml for services whose bounded context is declared in the
postgres-clusters bootstrap-SQL ConfigMaps.

For each service key under `services:` in the umbrella values.yaml, we
compute the candidate bc snake_case name as:
    bc = service_name.removesuffix("-service").replace("-", "_")

If `bc` is in the BCS set, we add (or overwrite) an envSecrets block in
values-dev.yaml under the same service key:
    envSecrets:
      DATABASE_URL:
        secretName: <bc-kebab>-db-dsn
        key: writer_url
      DATABASE_READ_URL:
        secretName: <bc-kebab>-db-dsn
        key: reader_url

The script reads service keys directly from each umbrella's values.yaml
(top-level only, fixed 2-space indent) and merges into values-dev.yaml as
text injection: services that already exist in values-dev.yaml get their
envSecrets block added/replaced, services that don't exist get a fresh
entry appended at the end of `services:`.

Idempotent: re-running yields the same file.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
HELM_APPS = ROOT / "infra" / "helm" / "apps"

# Single source of truth for the 59 bcs (snake_case). Mirrors
# infra/helm/infra/postgres-clusters/values.yaml dsnSecrets.bcs.
BCS = {
    # pg-schemas (23)
    "data_asset_catalog", "dataset_versioning", "lineage", "cdc_metadata",
    "model_catalog", "model_adapter", "model_lifecycle",
    "connector_management", "tenancy_organizations",
    "federation_product_exchange", "marketplace", "nexus", "sdk_generation",
    "solution_design", "code_repository_review", "document_reporting",
    "analytical_logic", "event_streaming", "ingestion_replication",
    "ai_application_generation", "document_intelligence", "mcp_orchestration",
    "scenario_simulation",
    # pg-policy (12)
    "identity_federation", "oauth_integration", "network_boundary", "cipher",
    "security_governance", "sds", "audit_compliance", "retention_policy",
    "lineage_deletion", "checkpoints_purpose", "telemetry_governance",
    "code_security_scanning",
    # pg-runtime-config (24)
    "agent_runtime", "notebook_runtime", "managed_workspace",
    "custom_endpoints", "developer_console", "app_builder",
    "application_composition", "spreadsheet_computation",
    "notification_alerting", "monitoring_rules", "health_check",
    "execution_observability", "workflow_automation", "automation_operations",
    "workflow_trace", "pipeline_authoring", "compute_modules_control_plane",
    "compute_modules_runtime", "sql_bi_gateway", "sql_warehousing",
    "tabular_analysis", "time_series_data", "report", "reindex_coordinator",
}

UMBRELLAS = [
    "of-platform", "of-data-engine", "of-ml-aip", "of-ontology", "of-apps-ops",
]

# Services with known build/code issues — skip wiring even if their bc
# is in BCS. They won't deploy, but we don't want a noisy half-config.
SKIP_SERVICES = {
    "lineage-service",
    "pipeline-schedule-service",
    "ontology-functions-service",
}


def service_to_bc(service_name: str) -> str:
    """kebab-case service name → snake_case bc name (drop -service suffix)."""
    base = service_name
    if base.endswith("-service"):
        base = base[: -len("-service")]
    return base.replace("-", "_")


def list_services(values_yaml: Path) -> list[str]:
    """Return service keys directly under top-level `services:` (2-space indent)."""
    text = values_yaml.read_text()
    services: list[str] = []
    in_services = False
    for line in text.splitlines():
        if line.rstrip() == "services:":
            in_services = True
            continue
        if in_services:
            # End of services block: any non-blank line that isn't indented
            # by 2 or more spaces, or the next top-level key.
            if line and not line.startswith(" ") and not line.startswith("\t"):
                in_services = False
                continue
            m = re.match(r"^  ([a-z][a-z0-9-]+):\s*$", line)
            if m:
                services.append(m.group(1))
    return services


ENV_SECRETS_BLOCK = (
    "    envSecrets:\n"
    "      DATABASE_URL:\n"
    "        secretName: {secret}\n"
    "        key: writer_url\n"
    "      DATABASE_READ_URL:\n"
    "        secretName: {secret}\n"
    "        key: reader_url\n"
)


def wire_values_dev(values_dev: Path, wires: dict[str, str]) -> tuple[int, int]:
    """
    Inject envSecrets into values-dev.yaml for each service in `wires`
    (service_name → secret_name).

    Strategy:
      1. If the file has a `services:` section, locate it and process each
         service block we own.
      2. For each wire, if the service block already exists, replace any
         existing `envSecrets:` block under it, otherwise append a fresh
         envSecrets block at the end of that service's block.
      3. For services not present in values-dev.yaml, append a new
         service entry under `services:` with just the envSecrets block.

    Returns (added_count, updated_count).
    """
    text = values_dev.read_text()
    if not text.endswith("\n"):
        text += "\n"

    # Find or create the services: block
    if not re.search(r"^services:\s*$", text, flags=re.MULTILINE):
        text += "\nservices:\n"

    added = 0
    updated = 0

    for service, secret in sorted(wires.items()):
        block = ENV_SECRETS_BLOCK.format(secret=secret)

        # Locate the service entry (2-space indent under services:)
        # Pattern: anchor on `^  <service>:\s*$` line.
        service_re = re.compile(rf"^  {re.escape(service)}:\s*$", flags=re.MULTILINE)
        m = service_re.search(text)

        if m is None:
            # Append a new entry to the end of the file (in services: section).
            new_entry = f"  {service}:\n{block}"
            # Ensure file ends with a newline before the new block
            if not text.endswith("\n"):
                text += "\n"
            text += new_entry
            added += 1
            continue

        # Find the end of this service's block (next 2-space-indented key
        # at same level, or EOF).
        start = m.end()
        # Find the next line that starts with a non-deeper indent.
        # Service blocks have nested keys at 4+ spaces. The block ends at
        # the next `^  <name>:` line or top-level key.
        next_section_re = re.compile(
            r"^(  [a-z][a-z0-9-]+:\s*$|[a-zA-Z][a-zA-Z0-9_-]*:|\s*$\Z)",
            flags=re.MULTILINE,
        )
        # Scan forward line by line.
        end = len(text)
        cursor = start
        while cursor < len(text):
            nl = text.find("\n", cursor)
            if nl == -1:
                end = len(text)
                break
            line = text[cursor + 1 : nl + 1] if cursor < nl else ""
            # We've consumed the newline at `start`; the next char is the
            # start of the next line.
            line_start = cursor + 1 if text[cursor:cursor+1] == "\n" else cursor
            line_end = text.find("\n", line_start)
            if line_end == -1:
                end = len(text)
                break
            current_line = text[line_start:line_end]
            cursor = line_end + 1
            if current_line == "":
                continue
            # Top-level key (no indent, ends with :)
            if re.match(r"^[a-zA-Z][a-zA-Z0-9_-]*:", current_line):
                end = line_start
                break
            # Sibling service key (2-space indent)
            if re.match(r"^  [a-z][a-z0-9-]+:\s*$", current_line):
                end = line_start
                break
        block_text = text[start:end]

        # Check for existing envSecrets in this service block and remove it.
        # An envSecrets block starts with `    envSecrets:` and continues
        # while indented at >= 6 spaces.
        existing_re = re.compile(
            r"(\n    envSecrets:\s*\n(?:(?:      [^\n]*\n)+))",
            flags=re.MULTILINE,
        )
        block_was_present = bool(existing_re.search(block_text))
        new_block_text = existing_re.sub("\n", block_text)

        # Append the new envSecrets block at the end of the service block.
        # Ensure trailing newline before insertion.
        new_block_text = new_block_text.rstrip("\n") + "\n" + block + (
            "\n" if block_text.endswith("\n\n") else ""
        )

        text = text[:start] + new_block_text + text[end:]

        if block_was_present:
            updated += 1
        else:
            added += 1

    values_dev.write_text(text)
    return added, updated


def main() -> int:
    total_wired = 0
    total_skipped_unknown_bc = 0
    summary: dict[str, dict] = {}

    for umbrella in UMBRELLAS:
        values_yaml = HELM_APPS / umbrella / "values.yaml"
        values_dev = HELM_APPS / umbrella / "values-dev.yaml"
        if not values_yaml.exists():
            print(f"[skip] {umbrella}: no values.yaml", file=sys.stderr)
            continue
        if not values_dev.exists():
            print(f"[skip] {umbrella}: no values-dev.yaml", file=sys.stderr)
            continue

        services = list_services(values_yaml)
        wires: dict[str, str] = {}
        skipped: list[tuple[str, str]] = []

        for svc in services:
            if svc in SKIP_SERVICES:
                skipped.append((svc, "known build issue"))
                continue
            bc = service_to_bc(svc)
            if bc not in BCS:
                skipped.append((svc, f"bc '{bc}' not in BCS"))
                total_skipped_unknown_bc += 1
                continue
            secret = bc.replace("_", "-") + "-db-dsn"
            wires[svc] = secret

        added, updated = wire_values_dev(values_dev, wires)
        total_wired += len(wires)
        summary[umbrella] = {
            "wired": sorted(wires.keys()),
            "skipped": skipped,
            "added": added,
            "updated": updated,
        }

    # Print summary
    print(f"\n=== wire_db_dsn summary ===")
    for umbrella, info in summary.items():
        print(f"\n[{umbrella}]")
        print(f"  wired ({len(info['wired'])}): added={info['added']} updated={info['updated']}")
        for s in info["wired"]:
            print(f"    + {s}")
        if info["skipped"]:
            print(f"  skipped ({len(info['skipped'])}):")
            for svc, reason in info["skipped"]:
                print(f"    - {svc}  ({reason})")
    print(f"\nTotal services wired: {total_wired}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
