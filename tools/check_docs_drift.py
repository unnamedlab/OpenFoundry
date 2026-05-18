#!/usr/bin/env python3
"""Fail when hand-written docs drift from repository inventory counts.

This intentionally checks only stable, code-derived inventory facts. Route
ownership and port drift are covered by Go tests next to the edge gateway so
those checks can call the router and config packages directly.
"""

from __future__ import annotations

import re
import sys
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


@dataclass(frozen=True)
class DocExpectation:
    path: str
    patterns: tuple[str, ...]


def count_dirs(name: str) -> int:
    base = ROOT / name
    return sum(1 for child in base.iterdir() if child.is_dir() and not child.name.startswith("."))


def require_patterns(expectations: list[DocExpectation]) -> list[str]:
    failures: list[str] = []
    for expectation in expectations:
        path = ROOT / expectation.path
        text = path.read_text(encoding="utf-8")
        for pattern in expectation.patterns:
            if not re.search(pattern, text, flags=re.MULTILINE):
                failures.append(f"{expectation.path}: missing pattern {pattern!r}")
    return failures


def main() -> int:
    service_count = count_dirs("services")
    lib_count = count_dirs("libs")
    proto_count = count_dirs("proto")

    expectations = [
        DocExpectation(
            "README.md",
            (
                rf"\*\*{service_count} service directories\*\*",
                rf"\*\*{lib_count} shared libraries\*\*",
            ),
        ),
        DocExpectation(
            "ARCHITECTURE.md",
            (
                rf"with {service_count} service directories under",
                rf"and {lib_count}\s+shared libraries under",
                r"docs/reference/repository-layout\.md",
            ),
        ),
        DocExpectation(
            "CLAUDE.md",
            (
                rf"services/\s+{service_count} service directories",
                rf"libs/\s+{lib_count} shared Go libraries",
            ),
        ),
        DocExpectation(
            "docs/reference/repository-layout.md",
            (
                rf"`services/` contains {service_count} service directories",
                rf"`libs/` contains {lib_count} cross-cutting Go packages",
                rf"\| `proto/` \| {proto_count} Protobuf domains;",
            ),
        ),
        DocExpectation(
            "docs/reference/documentation-code-gap-analysis.md",
            (
                rf"\| `find services .*` \| {service_count} service directories \|",
                rf"\| `find libs .*` \| {lib_count} library directories \|",
                rf"\| `find proto .*` \| {proto_count} protobuf domains \|",
            ),
        ),
    ]

    failures = require_patterns(expectations)
    if failures:
        print("Documentation inventory drift detected:", file=sys.stderr)
        for failure in failures:
            print(f"- {failure}", file=sys.stderr)
        print(
            "\nUpdate docs to match the code-derived counts or revise tools/check_docs_drift.py "
            "if a canonical docs location intentionally changed.",
            file=sys.stderr,
        )
        return 1

    print(
        "docs inventory ok: "
        f"services={service_count}, libs={lib_count}, proto={proto_count}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
