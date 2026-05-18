# syntax=docker/dockerfile:1.10
#
# pipeline-runner — Go binary that decodes a pipelineplan.Plan from
# the PIPELINE_PLAN_B64 env var the dispatcher (ADR-0045 Phase C.4.a)
# sets and executes it against Iceberg via libs/pipeline-runtime.
#
# ADR-0045 Phase C.5 — replaces the prior multi-stage image that
# bundled apache/spark (~600 MB) + the OpenFoundry Spark JAR
# (~50 MB) + the Go orchestrator. The new image is a distroless
# static Go binary (~25 MB) with no JVM and no Spark surface.
#
# Build context MUST be the openfoundry-go monorepo root.

ARG GO_VERSION=1.26.3
ARG ALPINE_VERSION=3.22
ARG DISTROLESS_IMAGE=gcr.io/distroless/static-debian12:nonroot

############################################################
# Stage 1 — modules: pre-warmed Go module graph.
############################################################
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS modules

ENV GOTOOLCHAIN=local \
    CGO_ENABLED=0 \
    GOFLAGS="-mod=readonly -buildvcs=false" \
    GOPROXY=https://proxy.golang.org,direct \
    GOSUMDB=sum.golang.org

WORKDIR /workspace

COPY --link go.mod go.sum ./
RUN --mount=type=cache,id=of-go-mod,target=/go/pkg/mod,sharing=locked \
    go mod download -x

############################################################
# Stage 2 — builder: compiles the pipeline-runner binary.
############################################################
FROM modules AS builder

ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN --mount=type=bind,source=.,target=/workspace,rw \
    --mount=type=cache,id=of-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=of-go-build,target=/root/.cache/go-build,sharing=locked \
    set -eux; \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
        go build -trimpath -buildvcs=false \
            -ldflags "-s -w -buildid= -extldflags=-static -X main.version=${VERSION}" \
            -tags "osusergo,netgo,static_build" \
            -o /out/pipeline-runner \
            ./services/pipeline-runner/cmd/pipeline-runner

############################################################
# Stage 3 — runtime: distroless static, nonroot.
############################################################
FROM ${DISTROLESS_IMAGE} AS runtime

ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.title="openfoundry-pipeline-runner" \
      org.opencontainers.image.description="Decodes a pipelineplan.Plan from PIPELINE_PLAN_B64 and runs it against Iceberg via libs/pipeline-runtime. Go port of the Scala/Spark pipeline-runner." \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="https://github.com/openfoundry/openfoundry-go" \
      org.opencontainers.image.vendor="OpenFoundry" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.base.name="gcr.io/distroless/static-debian12:nonroot"

COPY --from=builder /out/pipeline-runner /usr/local/bin/pipeline-runner

USER nonroot:nonroot
WORKDIR /home/nonroot
STOPSIGNAL SIGTERM

EXPOSE 9090

ENTRYPOINT ["/usr/local/bin/pipeline-runner"]
