#!/usr/bin/env python3
"""Render ADR-0012 S1 baseline evidence from k6 summary artifacts."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


def load_json(path: Path) -> dict[str, Any]:
    with path.open("r", encoding="utf-8") as handle:
        return json.load(handle)


def metric(summary: dict[str, Any], name: str, tags: dict[str, str]) -> dict[str, Any] | None:
    metrics = summary.get("metrics", {})
    prefix = f"{name}{{"
    for key, value in metrics.items():
        if key == name and not tags:
            return value
        if not key.startswith(prefix) or not key.endswith("}"):
            continue
        raw_tags = key[len(prefix) : -1]
        parsed: dict[str, str] = {}
        for item in raw_tags.split(","):
            if ":" not in item:
                continue
            tag, tag_value = item.split(":", 1)
            parsed[tag] = tag_value
        if all(parsed.get(tag) == expected for tag, expected in tags.items()):
            return value
    return None


def percentile(row: dict[str, Any] | None, key: str) -> str:
    if not row:
        return "missing metric"
    value = row.get("values", {}).get(key)
    if value is None:
        return "missing metric"
    return f"{float(value):.2f} ms"


def rate(summary: dict[str, Any]) -> str:
    row = summary.get("metrics", {}).get("iterations")
    value = row.get("values", {}).get("rate") if row else None
    if value is None:
        return "missing metric"
    return f"{float(value):.0f} RPS"


def dropped(summary: dict[str, Any]) -> str:
    row = summary.get("metrics", {}).get("dropped_iterations")
    value = row.get("values", {}).get("count") if row else 0
    return str(int(value or 0))


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--summary", required=True, type=Path)
    parser.add_argument("--metadata", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()

    summary = load_json(args.summary)
    metadata = load_json(args.metadata)

    rows = [
        (
            "A1 read by id (strong)",
            metric(
                summary,
                "http_req_duration",
                {"group": "::read-by-id", "consistency": "strong"},
            ),
        ),
        (
            "A1 read by id (eventual, cache hit)",
            metric(
                summary,
                "http_req_duration",
                {"group": "::read-by-id", "consistency": "eventual"},
            ),
        ),
        (
            "A2 list by type",
            metric(summary, "http_req_duration", {"group": "::read-by-type"}),
        ),
        (
            "A3 action execute",
            metric(summary, "http_req_duration", {"group": "::action-execute"}),
        ),
    ]

    run_id = metadata.get("run_id", "unknown-run")
    lines = [
        "### ADR-0012 S1 baseline result snippet",
        "",
        f"- Run id: `{run_id}`",
        f"- Date: `{metadata.get('date_utc', 'unknown')}`",
        f"- Commit: `{metadata.get('commit', 'unknown')}`",
        f"- Workspace dirty: `{metadata.get('workspace_dirty', 'unknown')}`",
        f"- Environment: `{metadata.get('environment', 'unknown')}`",
        f"- Dataset: `{metadata.get('dataset', 'unknown')}`",
        "",
        "| Operacion | p50 medido | p95 medido | p99 medido | Run id |",
        "|---|---:|---:|---:|---|",
    ]
    for label, row in rows:
        lines.append(
            f"| {label} | {percentile(row, 'p(50)')} | "
            f"{percentile(row, 'p(95)')} | {percentile(row, 'p(99)')} | `{run_id}` |"
        )
    lines.append(f"| Throughput (mix) | {rate(summary)} | dropped iterations | {dropped(summary)} | `{run_id}` |")
    lines.append("")

    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text("\n".join(lines), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
