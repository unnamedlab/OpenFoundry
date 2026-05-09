# syntax=docker/dockerfile:1.10
#
# pipeline-runner-spark — produces the fat JAR consumed by services/pipeline-runner.
#
# Two stages:
#   1. sbt-build — sbt assembly inside an Eclipse Temurin + Scala image.
#                 Caches sbt + ivy directories so Go-only changes do not
#                 trigger a Scala recompile.
#   2. exporter  — scratch stage that exposes only the assembled JAR. The
#                 Go orchestrator's Dockerfile copies this JAR via a
#                 `--from=pipeline-runner-spark` build arg.
#
# Build context MUST be the openfoundry-go monorepo root.
#
# Usage (standalone, mostly for CI):
#   docker buildx build -f services/pipeline-runner-spark/Dockerfile \
#       --target exporter --output type=local,dest=./out .
#   ls out/pipeline-runner-spark-*.jar

ARG SBT_IMAGE=sbtscala/scala-sbt:eclipse-temurin-17.0.10_7_1.10.0_2.12.19
ARG VERSION=dev

############################################################
# Stage 1 — sbt-build
############################################################
FROM ${SBT_IMAGE} AS sbt-build
ARG VERSION

WORKDIR /workspace

# Pre-warm sbt/ivy with just the build files so a code-only change does
# not re-resolve the dependency graph.
COPY services/pipeline-runner-spark/build.sbt          ./build.sbt
COPY services/pipeline-runner-spark/project/           ./project/
RUN --mount=type=cache,id=sbt-ivy,target=/root/.ivy2,sharing=locked \
    --mount=type=cache,id=sbt-cache,target=/root/.sbt,sharing=locked \
    --mount=type=cache,id=sbt-coursier,target=/root/.cache/coursier,sharing=locked \
    sbt --client -no-colors update || sbt -no-colors update

COPY services/pipeline-runner-spark/src/ ./src/

ENV VERSION=${VERSION}
RUN --mount=type=cache,id=sbt-ivy,target=/root/.ivy2,sharing=locked \
    --mount=type=cache,id=sbt-cache,target=/root/.sbt,sharing=locked \
    --mount=type=cache,id=sbt-coursier,target=/root/.cache/coursier,sharing=locked \
    sbt -no-colors "set assembly / assemblyJarName := \"pipeline-runner-spark.jar\"" assembly && \
    cp target/scala-2.12/pipeline-runner-spark.jar /workspace/pipeline-runner-spark.jar

############################################################
# Stage 2 — exporter (scratch w/ only the JAR)
############################################################
FROM scratch AS exporter
COPY --from=sbt-build /workspace/pipeline-runner-spark.jar /pipeline-runner-spark.jar
