# syntax=docker/dockerfile:1.7
#
# services/pipeline-runner/Dockerfile
#
# FASE 3 / Tarea 3.3 of docs/architecture/migration-plan-foundry-pattern-orchestration.md.
#
# Multi-stage build that produces a Spark/Iceberg/Lakekeeper-aware image
# used as the `image:` of every dynamically-generated SparkApplication CR
# (template: infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml).
#
# Stages:
#   1. `jars`     - download pinned Iceberg + iceberg-aws-bundle JARs from
#                   Maven Central. Tiny stage, cacheable forever as long
#                   as the version pins below don't change.
#   2. `builder`  - eclipse-temurin:17-jdk + sbt; runs `sbt package` to
#                   produce target/scala-2.12/pipeline-runner_2.12-0.1.0.jar.
#                   Stays out of the runtime image so the final size
#                   stays close to the apache/spark base.
#   3. `runtime`  - apache/spark:3.5.4-scala2.12-java17-python3-ubuntu;
#                   adds the three JARs to /opt/spark/jars/. No other
#                   layer changes. spark-submit is the entrypoint of the
#                   base image and is preserved.
#
# Bumping any version below requires bumping the matching pin in
# services/pipeline-runner/build.sbt and
# services/pipeline-runner/README.md (Version pins section).

ARG SPARK_VERSION=3.5.4
ARG SCALA_BIN=2.12
ARG ICEBERG_VERSION=1.5.2
ARG SBT_VERSION=1.10.0

# ---------------------------------------------------------------------
# Stage 1: download the Iceberg + AWS bundle JARs.
#
# We pull these in their own stage (not in the runtime stage) so the
# layer is cached independently of the Scala source and so the runtime
# stage doesn't need `curl` / `ca-certificates` extras.
# ---------------------------------------------------------------------
FROM alpine:3.20 AS jars
ARG SCALA_BIN
ARG ICEBERG_VERSION

RUN apk add --no-cache curl ca-certificates

WORKDIR /jars

# Iceberg → Spark binding (SQL extensions, SparkCatalog).
RUN curl -fsSL -o iceberg-spark-runtime.jar \
    "https://repo1.maven.org/maven2/org/apache/iceberg/iceberg-spark-runtime-3.5_${SCALA_BIN}/${ICEBERG_VERSION}/iceberg-spark-runtime-3.5_${SCALA_BIN}-${ICEBERG_VERSION}.jar"

# Lakekeeper REST catalog talks to S3-compatible object storage (Ceph
# RGW). The AWS SDK + URLConnection HTTP client live in this bundle.
RUN curl -fsSL -o iceberg-aws-bundle.jar \
    "https://repo1.maven.org/maven2/org/apache/iceberg/iceberg-aws-bundle/${ICEBERG_VERSION}/iceberg-aws-bundle-${ICEBERG_VERSION}.jar"

# ---------------------------------------------------------------------
# Stage 2: compile the Scala module with sbt.
#
# `sbt package` produces a thin jar (only the pipeline-runner classes;
# spark-sql is `Provided`). No assembly / fat jar — we rely on the
# runtime classpath.
# ---------------------------------------------------------------------
FROM eclipse-temurin:17-jdk-jammy AS builder
ARG SBT_VERSION

RUN apt-get update \
 && apt-get install -y --no-install-recommends curl ca-certificates gnupg \
 && rm -rf /var/lib/apt/lists/*

# Install sbt from sbt-rpm.bintray.com mirror (Eclipse Adoptium images
# don't ship sbt). Use the direct tarball from the sbt project so we
# don't depend on a third-party apt repo.
RUN curl -fsSL "https://github.com/sbt/sbt/releases/download/v${SBT_VERSION}/sbt-${SBT_VERSION}.tgz" \
    | tar -xz -C /opt \
 && ln -s /opt/sbt/bin/sbt /usr/local/bin/sbt

WORKDIR /workspace

# Prime the sbt + Coursier caches by resolving the build first; the
# subsequent COPY of the actual source then re-uses the cached deps.
COPY services/pipeline-runner/project/build.properties project/build.properties
COPY services/pipeline-runner/project/plugins.sbt project/plugins.sbt
COPY services/pipeline-runner/build.sbt build.sbt

# Cache mounts: keep ~/.sbt and ~/.cache/coursier across builds.
RUN --mount=type=cache,target=/root/.sbt,sharing=locked \
    --mount=type=cache,target=/root/.cache/coursier,sharing=locked \
    sbt --batch update

# Now copy the actual source and produce the jar.
COPY services/pipeline-runner/src/ src/
RUN --mount=type=cache,target=/root/.sbt,sharing=locked \
    --mount=type=cache,target=/root/.cache/coursier,sharing=locked \
    --mount=type=cache,target=/workspace/target,sharing=locked \
    sbt --batch package \
 && cp target/scala-2.12/pipeline-runner_2.12-*.jar /tmp/pipeline-runner.jar

# ---------------------------------------------------------------------
# Stage 3: runtime image.
#
# apache/spark:3.5.4-scala2.12-java17-python3-ubuntu ships:
#   - /opt/spark/{bin,sbin,jars,...}
#   - spark-submit as the entrypoint
#   - the `spark` user (UID 185) that the SparkApplication CR runs as
#
# We add nothing beyond the three JARs.
# ---------------------------------------------------------------------
FROM apache/spark:3.5.4-scala2.12-java17-python3-ubuntu AS runtime

# Drop to root only to install JARs into /opt/spark/jars/ (owned by
# root in the upstream image), then return to the `spark` user.
USER root

COPY --from=jars     /jars/iceberg-spark-runtime.jar  /opt/spark/jars/iceberg-spark-runtime-3.5_2.12-1.5.2.jar
COPY --from=jars     /jars/iceberg-aws-bundle.jar     /opt/spark/jars/iceberg-aws-bundle-1.5.2.jar
COPY --from=builder  /tmp/pipeline-runner.jar         /opt/spark/jars/pipeline-runner_2.12-0.1.0.jar

# Matches the upstream apache/spark image (UID 185).
USER 185

# spark-submit is inherited from the base image; the SparkApplication
# CR overrides ENTRYPOINT/CMD via spec.mainClass + spec.arguments.
