#!/usr/bin/env python3
"""Data residency registry + sqlx hot-path gate.

Validates `/.github/data-residency-allowlist.toml` against the workspace:

* every live migration directory is mapped exactly once,
* every mapped schema targets an allowed residency tier,
* mapped tables match the DDL actually present in the migrations,
* Postgres-bound schemas stay aligned with the CNPG bootstrap manifests,
* new `sqlx::query*` additions land only inside allowlisted files/tables.

The sqlx gate is diff-based and optional locally. In CI, pass
`--diff-base <sha-or-ref>` so the script inspects only lines newly added by
the change under review.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
import tomllib
from pathlib import Path

REGISTRY_PATH = Path(".github/data-residency-allowlist.toml")
BOOTSTRAP_PATHS = {
    "pg-schemas": Path("infra/k8s/platform/manifests/cnpg/clusters/pg-schemas-bootstrap-sql.yaml"),
    "pg-policy": Path("infra/k8s/platform/manifests/cnpg/clusters/pg-policy-bootstrap-sql.yaml"),
    "pg-runtime-config": Path("infra/k8s/platform/manifests/cnpg/clusters/pg-runtime-config-bootstrap-sql.yaml"),
}
QUERY_RE = re.compile(r"\bsqlx::query(?:_[A-Za-z0-9]+)?!?\b")
CREATE_TABLE_RE = re.compile(
    r"CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?"
    r"(?:(?:\"?[A-Za-z_][A-Za-z0-9_]*\"?)\.)?"
    r"(\"?[A-Za-z_][A-Za-z0-9_]*\"?)",
    re.IGNORECASE,
)
SQLX_LITERAL_PATTERNS = (
    re.compile(
        r"sqlx::query(?:_[A-Za-z0-9]+)?(?:::<[^\)]*>)?\(\s*"
        r'r(?P<hashes>#+)?\"(?P<sql>.*?)\"(?P=hashes)',
        re.IGNORECASE | re.DOTALL,
    ),
    re.compile(
        r'sqlx::query(?:_[A-Za-z0-9]+)?(?:::<[^\)]*>)?\(\s*'
        r'"(?P<sql>(?:[^"\\]|\\.)*)"',
        re.IGNORECASE | re.DOTALL,
    ),
)
SQLX_TABLE_RE = re.compile(
    r"\b(?:FROM|JOIN|UPDATE|INTO|DELETE\s+FROM|TRUNCATE\s+TABLE|TRUNCATE)\s+"
    r"(?:ONLY\s+)?"
    r"(?:(?:\"?[A-Za-z_][A-Za-z0-9_]*\"?)\.)?"
    r"(\"?[A-Za-z_][A-Za-z0-9_]*\"?)",
    re.IGNORECASE,
)
HUNK_RE = re.compile(r"^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@")
FORBIDDEN_HOT_TABLES = {
    "object_instances",
    "link_instances",
    "streaming_events",
    "dataset_transactions",
    "lineage_nodes",
    "refresh_tokens",
    "scoped_sessions",
}


def repo_root() -> Path:
    return Path(__file__).resolve().parent.parent.parent


def load_registry(root: Path) -> dict:
    path = root / REGISTRY_PATH
    if not path.is_file():
        raise FileNotFoundError(f"registry not found at {path}")
    with path.open("rb") as fh:
        return tomllib.load(fh)


def bootstrap_schemas(root: Path) -> dict[str, set[str]]:
    schemas: dict[str, set[str]] = {}
    for cluster, relpath in BOOTSTRAP_PATHS.items():
        path = root / relpath
        if not path.is_file():
            raise FileNotFoundError(f"bootstrap manifest not found at {path}")
        names = {
            match
            for match in re.findall(r"'([a-z_]+)'", path.read_text())
            if match != "PLACEHOLDER_ROTATE_VIA_EXTERNAL_SECRETS"
        }
        schemas[cluster] = names
    return schemas


def actual_migration_dirs(root: Path) -> set[str]:
    dirs = {
        str(path.parent.relative_to(root))
        for path in root.glob("services/*/migrations/*.sql")
    }
    dirs |= {
        str(path.parent.relative_to(root))
        for path in root.glob("libs/*/migrations/*.sql")
    }
    return dirs


def expected_component(migration_dir: str) -> str:
    parts = Path(migration_dir).parts
    if len(parts) < 3:
        raise ValueError(f"invalid migration_dir {migration_dir!r}")
    return str(Path(parts[0]) / parts[1])


def expected_service_and_schema(migration_dir: str) -> tuple[str, str]:
    parts = Path(migration_dir).parts
    if parts[0] == "services":
        service = parts[1]
        return service, service.removesuffix("-service").replace("-", "_")
    if parts[0] == "libs":
        crate = parts[1]
        return crate, crate.replace("-", "_")
    raise ValueError(f"unsupported migration_dir root in {migration_dir!r}")


def extract_tables(root: Path, migration_dir: str) -> list[str]:
    tables: list[str] = []
    seen: set[str] = set()
    for sql in sorted((root / migration_dir).glob("*.sql")):
        for raw in CREATE_TABLE_RE.findall(sql.read_text()):
            table = raw.strip('"')
            if table not in seen:
                seen.add(table)
                tables.append(table)
    return tables


def extract_sqlx_tables(path: Path) -> set[str]:
    tables: set[str] = set()
    text = path.read_text(errors="ignore")
    for pattern in SQLX_LITERAL_PATTERNS:
        for match in pattern.finditer(text):
            sql = match.group("sql")
            for raw in SQLX_TABLE_RE.findall(sql):
                tables.add(raw.strip('"'))
    return tables


def validate_registry(root: Path, registry: dict, bootstraps: dict[str, set[str]]) -> list[str]:
    errors: list[str] = []
    config = registry.get("registry", {})
    entries = registry.get("entries", [])
    hot_path = registry.get("sqlx_hot_path", {})
    allowed_residencies = set(config.get("residency_targets", []))
    forbidden_hot_tables = set(config.get("forbidden_hot_tables", FORBIDDEN_HOT_TABLES))
    expected_count = config.get("expected_migration_dirs")
    actual_dirs = actual_migration_dirs(root)

    if not isinstance(entries, list):
        return ["registry.entries must be a TOML array-of-tables"]
    if expected_count != len(actual_dirs):
        errors.append(
            f"registry.expected_migration_dirs={expected_count} does not match "
            f"actual migration dir count {len(actual_dirs)}"
        )
    if len(entries) != len(actual_dirs):
        errors.append(
            f"registry contains {len(entries)} entries but repo has "
            f"{len(actual_dirs)} migration dirs"
        )

    shared_allowed_files = hot_path.get("shared_allowed_files", [])
    for relpath in shared_allowed_files:
        if not (root / relpath).is_file():
            errors.append(f"shared sqlx allowlist file does not exist: {relpath}")

    seen_dirs: set[str] = set()
    seen_components: set[str] = set()
    seen_services: set[str] = set()
    seen_sqlx_files: set[Path] = set()

    for idx, entry in enumerate(entries, start=1):
        label = f"entries[{idx}]"
        migration_dir = entry.get("migration_dir")
        component = entry.get("component")
        service = entry.get("service")
        schema = entry.get("schema")
        residency = entry.get("residency")
        allow_new_sqlx = entry.get("allow_new_sqlx")
        legacy_archive_tables = entry.get("legacy_archive_tables", [])
        tables = entry.get("tables")

        if not all(isinstance(v, str) for v in (migration_dir, component, service, schema, residency)):
            errors.append(f"{label}: component/service/migration_dir/schema/residency must be strings")
            continue
        if allow_new_sqlx is not None and not isinstance(allow_new_sqlx, dict):
            errors.append(f"{label}: allow_new_sqlx must be an inline table with files/tables")
        if not isinstance(legacy_archive_tables, list) or not all(
            isinstance(t, str) for t in legacy_archive_tables
        ):
            errors.append(f"{label}: legacy_archive_tables must be an array of strings")
        if not isinstance(tables, list) or not all(isinstance(t, str) for t in tables):
            errors.append(f"{label}: tables must be an array of strings")
            continue
        if residency not in allowed_residencies:
            errors.append(
                f"{label}: residency {residency!r} is not one of "
                f"{sorted(allowed_residencies)}"
            )
        if migration_dir in seen_dirs:
            errors.append(f"{label}: duplicate migration_dir {migration_dir}")
        seen_dirs.add(migration_dir)
        if component in seen_components:
            errors.append(f"{label}: duplicate component {component}")
        seen_components.add(component)
        if service in seen_services:
            errors.append(f"{label}: duplicate service {service}")
        seen_services.add(service)
        if migration_dir not in actual_dirs:
            errors.append(f"{label}: migration_dir not found in repo: {migration_dir}")
            continue

        derived_component = expected_component(migration_dir)
        if component != derived_component:
            errors.append(
                f"{label}: component {component!r} does not match migration_dir "
                f"{migration_dir!r} (expected {derived_component!r})"
            )

        derived_service, derived_schema = expected_service_and_schema(migration_dir)
        if service != derived_service:
            errors.append(
                f"{label}: service {service!r} does not match migration_dir "
                f"{migration_dir!r} (expected {derived_service!r})"
            )
        if schema != derived_schema:
            errors.append(
                f"{label}: schema {schema!r} does not match migration_dir "
                f"{migration_dir!r} (expected {derived_schema!r})"
            )

        forbidden_tables = set(tables) & forbidden_hot_tables
        if forbidden_tables and residency != "legacy-archive":
            missing_legacy = sorted(forbidden_tables - set(legacy_archive_tables))
            if missing_legacy:
                errors.append(
                    f"{label}: forbidden hot tables {missing_legacy} require "
                    "legacy_archive_tables or residency='legacy-archive'"
                )
        extra_legacy = sorted(set(legacy_archive_tables) - set(tables))
        if extra_legacy:
            errors.append(
                f"{label}: legacy_archive_tables references unknown tables {extra_legacy}"
            )

        if allow_new_sqlx:
            files = allow_new_sqlx.get("files")
            allowed_sqlx_tables = allow_new_sqlx.get("tables")
            legacy_archive = allow_new_sqlx.get("legacy_archive", False)
            if not isinstance(files, list) or not all(isinstance(v, str) for v in files):
                errors.append(f"{label}: allow_new_sqlx.files must be an array of strings")
                files = []
            if not isinstance(allowed_sqlx_tables, list) or not all(
                isinstance(v, str) for v in allowed_sqlx_tables
            ):
                errors.append(f"{label}: allow_new_sqlx.tables must be an array of strings")
                allowed_sqlx_tables = []
            if not isinstance(legacy_archive, bool):
                errors.append(f"{label}: allow_new_sqlx.legacy_archive must be a boolean")
                legacy_archive = False
            if not files:
                errors.append(f"{label}: allow_new_sqlx.files cannot be empty")
            if not allowed_sqlx_tables:
                errors.append(f"{label}: allow_new_sqlx.tables cannot be empty")
            if not residency.startswith("pg-") and not legacy_archive:
                errors.append(
                    f"{label}: allow_new_sqlx is only valid for pg-* residencies unless "
                    "allow_new_sqlx.legacy_archive=true"
                )
            src_dir = root / component / "src"
            if not src_dir.is_dir():
                errors.append(f"{label}: allow_new_sqlx declared but missing src dir {src_dir}")
            unknown_allowlist_tables = sorted(set(allowed_sqlx_tables) - set(tables))
            if unknown_allowlist_tables:
                errors.append(
                    f"{label}: allow_new_sqlx.tables references unknown tables "
                    f"{unknown_allowlist_tables}"
                )
            forbidden_allowlist_tables = sorted(
                set(allowed_sqlx_tables) & forbidden_hot_tables
            )
            if forbidden_allowlist_tables and not legacy_archive:
                errors.append(
                    f"{label}: allow_new_sqlx.tables cannot include forbidden hot tables "
                    f"{forbidden_allowlist_tables}"
                )
            if legacy_archive:
                allowed_legacy_tables = (
                    set(tables) if residency == "legacy-archive" else set(legacy_archive_tables)
                )
                unexpected_tables = sorted(set(allowed_sqlx_tables) - allowed_legacy_tables)
                if unexpected_tables:
                    errors.append(
                        f"{label}: allow_new_sqlx.legacy_archive=true may only target "
                        f"legacy archive tables, found {unexpected_tables}"
                    )

            for relpath in files:
                path = root / relpath
                if not path.is_file():
                    errors.append(f"{label}: allow_new_sqlx file does not exist: {relpath}")
                    continue
                if src_dir.is_dir() and not path.is_relative_to(src_dir):
                    errors.append(
                        f"{label}: allow_new_sqlx file {relpath} must stay inside {src_dir.relative_to(root)}"
                    )
                if path in seen_sqlx_files:
                    errors.append(f"{label}: duplicate allow_new_sqlx file {relpath}")
                seen_sqlx_files.add(path)
                file_tables = extract_sqlx_tables(path)
                unexpected_file_tables = sorted(file_tables - set(allowed_sqlx_tables))
                if unexpected_file_tables:
                    errors.append(
                        f"{label}: allow_new_sqlx drift in {relpath}; file queries tables "
                        f"{unexpected_file_tables} outside allow_new_sqlx.tables"
                    )

        actual_tables = extract_tables(root, migration_dir)
        if set(actual_tables) != set(tables):
            missing = sorted(set(actual_tables) - set(tables))
            extra = sorted(set(tables) - set(actual_tables))
            detail = []
            if missing:
                detail.append(f"missing {missing}")
            if extra:
                detail.append(f"extra {extra}")
            errors.append(
                f"{label}: table mapping drift for {migration_dir}: "
                + ", ".join(detail)
            )

        if residency in bootstraps:
            # `outbox` is provisioned by its own init script rather than the
            # per-service CNPG bootstrap manifest.
            if schema == "outbox" and residency == "pg-policy":
                continue
            if schema not in bootstraps[residency]:
                errors.append(
                    f"{label}: schema {schema!r} is not present in "
                    f"{residency} bootstrap manifest"
                )

    missing_dirs = sorted(actual_dirs - seen_dirs)
    extra_dirs = sorted(seen_dirs - actual_dirs)
    if missing_dirs:
        errors.append(f"registry missing migration_dir entries: {missing_dirs}")
    if extra_dirs:
        errors.append(f"registry lists unknown migration dirs: {extra_dirs}")

    return errors


def allowed_sqlx_paths(root: Path, registry: dict) -> tuple[dict[Path, set[str]], set[Path]]:
    files: dict[Path, set[str]] = {}
    shared_files: set[Path] = set()

    for entry in registry.get("entries", []):
        allow_new_sqlx = entry.get("allow_new_sqlx") or {}
        for relpath in allow_new_sqlx.get("files", []):
            files[root / relpath] = set(allow_new_sqlx.get("tables", []))

    hot_path = registry.get("sqlx_hot_path", {})
    for relpath in hot_path.get("shared_allowed_files", []):
        shared_files.add(root / relpath)

    return files, shared_files


def diff_added_sqlx_violations(
    root: Path,
    diff_base: str | None,
    allowed_files: dict[Path, set[str]],
    shared_allowed_files: set[Path],
) -> list[str]:
    if not diff_base or not diff_base.strip() or diff_base == "0000000000000000000000000000000000000000":
        return []

    cmd = [
        "git",
        "diff",
        "--unified=0",
        "--no-color",
        f"{diff_base}...HEAD",
        "--",
        "libs",
        "services",
    ]
    result = subprocess.run(
        cmd,
        cwd=root,
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        return [
            "could not inspect git diff for sqlx file/table gate: "
            + result.stderr.strip()
        ]

    violations: list[str] = []
    files_with_new_sqlx: set[Path] = set()
    current_file: Path | None = None
    current_lineno: int | None = None

    for raw in result.stdout.splitlines():
        if raw.startswith("+++ b/"):
            rel = raw[6:]
            current_file = root / rel
            current_lineno = None
            continue
        if raw.startswith("@@ "):
            match = HUNK_RE.match(raw)
            current_lineno = int(match.group(1)) if match else None
            continue
        if raw.startswith("+") and not raw.startswith("+++"):
            if current_file is not None and current_lineno is not None:
                line = raw[1:]
                if should_check_sqlx_line(current_file, line) and QUERY_RE.search(line):
                    files_with_new_sqlx.add(current_file)
                    if not is_allowlisted_sqlx_path(
                        current_file, allowed_files, shared_allowed_files
                    ):
                        relpath = current_file.relative_to(root)
                        violations.append(
                            f"{relpath}:{current_lineno}: new sqlx::query* outside "
                            f"data-residency file/table allowlist"
                        )
                current_lineno += 1
            continue
        if raw.startswith(" ") and current_lineno is not None:
            current_lineno += 1

    for path in sorted(files_with_new_sqlx):
        if path in shared_allowed_files or path not in allowed_files:
            continue
        unexpected_tables = sorted(extract_sqlx_tables(path) - allowed_files[path])
        if unexpected_tables:
            relpath = path.relative_to(root)
            violations.append(
                f"{relpath}: sqlx table allowlist drift; file queries tables "
                f"{unexpected_tables} outside allow_new_sqlx.tables"
            )

    return violations


def should_check_sqlx_line(path: Path, line: str) -> bool:
    rel = path.as_posix()
    return (
        path.suffix == ".rs"
        and "/src/" in rel
        and not rel.endswith("/mod.rs.orig")
        and "sqlx::query" in line
    )


def is_allowlisted_sqlx_path(
    path: Path, allowed_files: dict[Path, set[str]], shared_allowed_files: set[Path]
) -> bool:
    if path in shared_allowed_files:
        return True
    return path in allowed_files


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--diff-base",
        default=None,
        help="Git base ref/SHA used to detect newly added sqlx::query* lines",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    root = repo_root()

    try:
        registry = load_registry(root)
        bootstraps = bootstrap_schemas(root)
    except (FileNotFoundError, tomllib.TOMLDecodeError, ValueError) as exc:
        print(f"Data residency lint FAILED: {exc}", file=sys.stderr)
        return 2

    errors = validate_registry(root, registry, bootstraps)
    allowed_files, shared_allowed_files = allowed_sqlx_paths(root, registry)
    errors.extend(
        diff_added_sqlx_violations(root, args.diff_base, allowed_files, shared_allowed_files)
    )

    if errors:
        print("Data residency lint FAILED:", file=sys.stderr)
        for err in errors:
            print(f"  - {err}", file=sys.stderr)
        return 1

    print(
        "Data residency lint OK: "
        f"{len(registry.get('entries', []))} mapped migration dirs, "
        f"{len(allowed_files)} sqlx allowlisted file(s), "
        f"{len(shared_allowed_files)} shared sqlx exception file(s)."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
