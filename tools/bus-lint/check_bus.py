#!/usr/bin/env python3
"""Bus contract lint — see ADR-0011.

Walks ``/services/*/Cargo.toml`` and verifies that each service's actual
dependency on ``event-bus-control`` and/or ``event-bus-data`` matches the
allowlist declared in ``/.github/bus-allowlist.yaml``.

Fails (exit code 1) on any drift:

* a service depends on a bus crate that is not declared for it,
* a service is missing from the allowlist while depending on either bus,
* the allowlist lists a bus for a service that no longer depends on it
  (stale entry).

No third-party dependencies. Uses only the Python 3 standard library.

The ``Cargo.toml`` parsing is intentionally a small, line-based scan rather
than a full TOML parser: a few service crates in this workspace currently
declare the same dependency table twice (e.g. two
``[dependencies.async-trait]`` blocks), which strict TOML parsers reject.
Cargo itself tolerates this, and we only need to know whether a specific
crate name appears as a dependency under one of the dependency tables — so
a focused scanner is both sufficient and more robust.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

BUS_CRATES = ("event-bus-control", "event-bus-data")
BUS_KEYS = {"event-bus-control": "control", "event-bus-data": "data"}
VALID_BUSES = {"control", "data"}

# Dependency-table prefixes we care about. Cargo also supports
# ``target.<cfg>.dependencies`` etc., but no service in this workspace uses
# them for the bus crates and they are out of scope for this contract.
DEP_TABLES = ("dependencies", "build-dependencies", "dev-dependencies")

_TABLE_HEADER_RE = re.compile(r"^\s*\[([^\]]+)\]\s*$")


def repo_root() -> Path:
    # tools/bus-lint/check_bus.py -> repo root is two levels up.
    return Path(__file__).resolve().parent.parent.parent


def _strip_comment(line: str) -> str:
    """Drop a trailing ``# ...`` TOML comment.

    Tracks basic string state so a ``#`` inside a quoted value is not
    treated as a comment, with proper handling of escaped quotes inside
    basic strings (``"..\\"..."``). Literal strings (``'...'``) do not
    process escapes per the TOML spec.
    """
    in_str = False
    quote = ""
    escaped = False
    for i, ch in enumerate(line):
        if in_str:
            if quote == '"' and escaped:
                escaped = False
                continue
            if quote == '"' and ch == "\\":
                escaped = True
                continue
            if ch == quote:
                in_str = False
                escaped = False
            continue
        if ch in ('"', "'"):
            in_str = True
            quote = ch
            continue
        if ch == "#":
            return line[:i]
    return line


def detect_buses(cargo_toml: Path) -> set[str]:
    """Return the set of bus keys ({"control", "data"}) the crate depends on.

    Recognises three Cargo dependency forms under any of ``[dependencies]``,
    ``[build-dependencies]``, ``[dev-dependencies]``:

    * detached table:           ``[dependencies.event-bus-control]``
    * inline simple value:      ``event-bus-control = "0.1"``
    * inline dotted key:        ``event-bus-control.workspace = true``
    """
    used: set[str] = set()
    current_table: str | None = None

    # Pre-build per-crate matchers for inline forms.
    inline_patterns = {
        crate: re.compile(
            rf"^\s*{re.escape(crate)}\s*(?:=|\.[A-Za-z0-9_-]+\s*=)"
        )
        for crate in BUS_CRATES
    }

    for raw in cargo_toml.read_text().splitlines():
        line = _strip_comment(raw).rstrip()
        if not line.strip():
            continue

        header = _TABLE_HEADER_RE.match(line)
        if header:
            name = header.group(1).strip()
            current_table = name
            # Detached table form: e.g. [dependencies.event-bus-control].
            for crate in BUS_CRATES:
                for table in DEP_TABLES:
                    if name == f"{table}.{crate}":
                        used.add(BUS_KEYS[crate])
            continue

        # Inline form only counts when we're inside a relevant table.
        if current_table in DEP_TABLES:
            for crate, pattern in inline_patterns.items():
                if pattern.match(line):
                    used.add(BUS_KEYS[crate])

    return used


def parse_allowlist(path: Path) -> dict[str, set[str]]:
    """Parse the minimal YAML subset used by ``.github/bus-allowlist.yaml``.

    The file is expected to look like::

        services:
          some-service:
            - control
          other-service:
            - control
            - data

    Comments (``#``) and blank lines are ignored. Anything outside that
    shape raises ``ValueError`` — we want loud failures rather than silent
    misparsing of the contract file.
    """
    services: dict[str, set[str]] = {}
    in_services = False
    current: str | None = None

    for lineno, raw in enumerate(path.read_text().splitlines(), start=1):
        # Strip inline comments and trailing whitespace.
        line = raw.split("#", 1)[0].rstrip()
        if not line.strip():
            continue

        stripped = line.lstrip()
        indent = len(line) - len(stripped)

        if indent == 0:
            if stripped == "services:":
                in_services = True
                current = None
                continue
            raise ValueError(
                f"{path}:{lineno}: unexpected top-level key {stripped!r}; "
                f"only 'services:' is supported"
            )

        if not in_services:
            raise ValueError(
                f"{path}:{lineno}: indented content outside 'services:' block"
            )

        if stripped.startswith("- "):
            if current is None:
                raise ValueError(
                    f"{path}:{lineno}: list item with no parent service"
                )
            bus = stripped[2:].strip()
            if bus not in VALID_BUSES:
                raise ValueError(
                    f"{path}:{lineno}: unknown bus {bus!r} for service "
                    f"{current!r}; expected one of {sorted(VALID_BUSES)}"
                )
            services[current].add(bus)
            continue

        if stripped.endswith(":"):
            name = stripped[:-1].strip()
            if not name:
                raise ValueError(f"{path}:{lineno}: empty service name")
            if name in services:
                raise ValueError(
                    f"{path}:{lineno}: duplicate service entry {name!r}"
                )
            services[name] = set()
            current = name
            continue

        raise ValueError(f"{path}:{lineno}: cannot parse line: {raw!r}")

    return services


def main() -> int:
    root = repo_root()
    allowlist_path = root / ".github" / "bus-allowlist.yaml"
    services_dir = root / "services"

    if not allowlist_path.is_file():
        print(f"error: allowlist not found at {allowlist_path}", file=sys.stderr)
        return 2
    if not services_dir.is_dir():
        print(f"error: services dir not found at {services_dir}", file=sys.stderr)
        return 2

    allowlist = parse_allowlist(allowlist_path)
    errors: list[str] = []

    seen_services: set[str] = set()

    for cargo in sorted(services_dir.glob("*/Cargo.toml")):
        service = cargo.parent.name
        seen_services.add(service)
        used = detect_buses(cargo)
        declared = allowlist.get(service, set())

        # Service uses a bus but is missing from the allowlist entirely.
        if used and service not in allowlist:
            errors.append(
                f"{service}: depends on bus(es) {sorted(used)} but is not "
                f"listed in .github/bus-allowlist.yaml"
            )
            continue

        undeclared = used - declared
        stale = declared - used

        if undeclared:
            errors.append(
                f"{service}: depends on bus(es) {sorted(undeclared)} not "
                f"declared in .github/bus-allowlist.yaml (declared: "
                f"{sorted(declared) or 'none'})"
            )
        if stale:
            errors.append(
                f"{service}: allowlist declares bus(es) {sorted(stale)} "
                f"but Cargo.toml does not depend on the matching crate"
            )

    # Allowlist references a service that no longer exists.
    for service in sorted(set(allowlist) - seen_services):
        errors.append(
            f"{service}: present in .github/bus-allowlist.yaml but no "
            f"matching services/{service}/Cargo.toml found"
        )

    if errors:
        print("Bus contract lint FAILED (see ADR-0011):", file=sys.stderr)
        for err in errors:
            print(f"  - {err}", file=sys.stderr)
        return 1

    print(
        f"Bus contract lint OK: {len(seen_services)} service(s) checked, "
        f"{sum(1 for s in seen_services if allowlist.get(s))} "
        f"with declared bus dependencies."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
