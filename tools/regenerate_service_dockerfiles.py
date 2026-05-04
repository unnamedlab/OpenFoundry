#!/usr/bin/env python3
"""Regenerate service Dockerfiles with robust BuildKit cache mounts.

Rationale:
- `cargo-chef` proved fragile for this workspace under Docker/Alpine.
- BuildKit cache mounts for cargo registry/git and a per-service target dir
  keep rebuilds incremental without depending on cargo-chef skeleton builds.
- Any source change still triggers `cargo build`; cached artifacts make it fast.
"""

from __future__ import annotations

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SERVICES_DIR = ROOT / "services"

# Services whose Dockerfile is NOT a Rust workspace build and therefore
# must not be passed through extract_meta() / render(). The script
# leaves these untouched. Add new entries here when introducing
# non-Rust services that still live under services/.
NON_RUST_SERVICES: set[str] = {
    # FASE 3 / Tarea 3.3: Scala/SBT image used as the `image:` of every
    # dynamically-generated SparkApplication CR. Multi-stage build on
    # top of apache/spark:3.5.4 — see services/pipeline-runner/README.md.
    "pipeline-runner",
}

PORT_RE = re.compile(r"^ENV PORT=(\d+)", re.MULTILINE)
PKG_RE = re.compile(r"(?:cargo build|cargo chef cook) [^\n]*-p\s+(\S+)")
WORKDIR_RE = re.compile(r"^WORKDIR\s+(\S+)\s*$", re.MULTILINE)
CONFIG_RE = re.compile(r"COPY --from=(?:builder|build) /workspace/services/([^/]+)/config")

# Per-service build overrides.
# - extra_build_apk: additional Alpine packages installed in the builder stage
#   (e.g. native deps for vendored C libraries built via cmake).
# - extra_runtime_apk: additional Alpine packages installed in the runtime
#   stage (e.g. dynamic libs needed at startup).
# - cargo_features: comma-separated cargo feature list passed via --features.
# - rust_flags: value for `ENV RUSTFLAGS=...` in the builder stage. Used by
#   rdkafka services to disable musl static-CRT so cyrus-sasl/openssl/curl
#   link dynamically — the alternative (chasing *-static for every cyrus-sasl
#   plugin: sqlite-static, openldap-static, ...) is fragile and Alpine doesn't
#   ship all of them. The runtime stage already installs the matching .so
#   packages.
SERVICE_OVERRIDES: dict[str, dict[str, str]] = {
    # event-streaming-service: enable real Kafka backend (rdkafka with
    # vendored librdkafka built through cmake) for production images.
    "event-streaming-service": {
        "extra_build_apk": "cmake perl zlib-dev zstd-dev cyrus-sasl-dev curl-dev linux-headers",
        "extra_runtime_apk": "zlib zstd-libs cyrus-sasl libcurl",
        "rust_flags": "-C target-feature=-crt-static",
        "cargo_features": "kafka-rdkafka",
    },
    # Data-connection plane connectors: enable live Kafka I/O
    # (test_connection / discover_sources / query_virtual_table) backed by
    # rdkafka. Same native deps as event-streaming-service.
    "connector-management-service": {
        "extra_build_apk": "cmake perl zlib-dev zstd-dev cyrus-sasl-dev curl-dev linux-headers",
        "extra_runtime_apk": "zlib zstd-libs cyrus-sasl libcurl",
        "rust_flags": "-C target-feature=-crt-static",
        "cargo_features": "kafka-rdkafka",
    },
    "ingestion-replication-service": {
        "extra_build_apk": "protobuf-dev cmake perl zlib-dev zstd-dev cyrus-sasl-dev curl-dev linux-headers",
        "extra_runtime_apk": "zlib zstd-libs cyrus-sasl libcurl",
        "rust_flags": "-C target-feature=-crt-static",
        "cargo_features": "kafka-rdkafka",
    },
    "media-sets-service": {
        "extra_build_apk": "protobuf-dev",
    },
    "ontology-indexer": {
        "extra_build_apk": "cmake perl zlib-dev zstd-dev cyrus-sasl-dev curl-dev linux-headers",
        "extra_runtime_apk": "zlib zstd-libs cyrus-sasl libcurl",
        "rust_flags": "-C target-feature=-crt-static",
        "cargo_features": "runtime,opensearch",
    },
    "audit-sink": {
        "extra_build_apk": "cmake perl zlib-dev zstd-dev cyrus-sasl-dev curl-dev linux-headers",
        "extra_runtime_apk": "zlib zstd-libs cyrus-sasl libcurl",
        "rust_flags": "-C target-feature=-crt-static",
        "cargo_features": "runtime",
    },
    "ai-sink": {
        "extra_build_apk": "cmake perl zlib-dev zstd-dev cyrus-sasl-dev curl-dev linux-headers",
        "extra_runtime_apk": "zlib zstd-libs cyrus-sasl libcurl",
        "rust_flags": "-C target-feature=-crt-static",
        "cargo_features": "runtime",
    },
    "virtual-table-service": {
        "extra_build_apk": "cmake perl zlib-dev zstd-dev cyrus-sasl-dev curl-dev linux-headers",
        "extra_runtime_apk": "zlib zstd-libs cyrus-sasl libcurl",
        "rust_flags": "-C target-feature=-crt-static",
        "cargo_features": "kafka-rdkafka",
    },
    # FASE 4 / Tarea 4.2: Kafka-triggered Cassandra reindex coordinator.
    # Pulls rdkafka through `event-bus-data` under the `runtime` feature,
    # plus the scylla driver through `cassandra-kernel`. Same native
    # build deps as `ontology-indexer`.
    "reindex-coordinator-service": {
        "extra_build_apk": "cmake perl zlib-dev zstd-dev cyrus-sasl-dev curl-dev linux-headers",
        "extra_runtime_apk": "zlib zstd-libs cyrus-sasl libcurl",
        "rust_flags": "-C target-feature=-crt-static",
        "cargo_features": "runtime",
    },
    # Pulls rdkafka transitively through libs/event-bus-data (no feature
    # flag — the dependency is unconditional), so it needs the same
    # native packages as the explicit Kafka services.
    "identity-federation-service": {
        "extra_build_apk": "cmake perl zlib-dev zstd-dev cyrus-sasl-dev curl-dev linux-headers",
        "extra_runtime_apk": "zlib zstd-libs cyrus-sasl libcurl",
        "rust_flags": "-C target-feature=-crt-static",
    },
}

TEMPLATE = """# syntax=docker/dockerfile:1.7
# Auto-generated by tools/regenerate_service_dockerfiles.py
# Do not edit by hand - update the script and re-run it instead.

FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates python3{extra_build_apk}
ENV PYO3_PYTHON=python3
ENV PYO3_USE_ABI3_FORWARD_COMPATIBILITY=1
{rust_flags_env}

COPY . .
RUN --mount=type=cache,target=/usr/local/cargo/registry,sharing=locked \\
    --mount=type=cache,target=/usr/local/cargo/git,sharing=locked \\
    --mount=type=cache,target=/workspace/target,id={pkg}-target,sharing=locked \\
    cargo build --locked --profile docker-release -p {pkg}{cargo_features} \
 && cp /workspace/target/docker-release/{pkg} /tmp/{pkg} \
 && strip /tmp/{pkg}

FROM alpine:3.23 AS runtime
WORKDIR {workdir}

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3{extra_runtime_apk}

ENV HOST=0.0.0.0
ENV PORT={port}

COPY --from=builder /tmp/{pkg} /usr/local/bin/{pkg}
{config_copy}EXPOSE {port}
CMD [\"/usr/local/bin/{pkg}\"]
"""


def extract_meta(dockerfile: Path) -> dict[str, str | bool]:
    content = dockerfile.read_text()

    pkg_match = PKG_RE.search(content)
    port_match = PORT_RE.search(content)
    if not pkg_match or not port_match:
        raise ValueError(f"Cannot parse package/port from {dockerfile}")

    workdirs = WORKDIR_RE.findall(content)
    runtime_workdir = workdirs[-1] if workdirs else "/app"
    has_config = bool(CONFIG_RE.search(content))

    return {
        "pkg": pkg_match.group(1),
        "port": port_match.group(1),
        "workdir": runtime_workdir,
        "has_config": has_config,
    }


def render(meta: dict[str, str | bool]) -> str:
    pkg = str(meta["pkg"])
    port = str(meta["port"])
    workdir = str(meta["workdir"])
    has_config = bool(meta["has_config"])
    config_copy = (
        f"COPY --from=builder /workspace/services/{pkg}/config ./config\n"
        if has_config
        else ""
    )
    overrides = SERVICE_OVERRIDES.get(pkg, {})
    extra_build = overrides.get("extra_build_apk", "")
    extra_runtime = overrides.get("extra_runtime_apk", "")
    cargo_features = overrides.get("cargo_features", "")
    rust_flags = overrides.get("rust_flags", "")
    return TEMPLATE.format(
        pkg=pkg,
        port=port,
        workdir=workdir,
        config_copy=config_copy,
        extra_build_apk=(f" {extra_build}" if extra_build else ""),
        extra_runtime_apk=(f" {extra_runtime}" if extra_runtime else ""),
        cargo_features=(f" --features {cargo_features}" if cargo_features else ""),
        rust_flags_env=(f'ENV RUSTFLAGS="{rust_flags}"\n' if rust_flags else ""),
    )


def main() -> None:
    count = 0
    skipped = 0
    for service_dir in sorted(SERVICES_DIR.iterdir()):
        dockerfile = service_dir / "Dockerfile"
        if not dockerfile.exists():
            continue
        if service_dir.name in NON_RUST_SERVICES:
            skipped += 1
            continue
        meta = extract_meta(dockerfile)
        dockerfile.write_text(render(meta))
        count += 1
    print(f"Rewrote {count} service Dockerfiles (skipped {skipped} non-Rust)")


if __name__ == "__main__":
    main()
