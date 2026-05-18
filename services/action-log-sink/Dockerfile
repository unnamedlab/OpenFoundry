# syntax=docker/dockerfile:1.10
#
# action-log-sink — Go port of ActionLogStreamSink (Scala / Spark
# Structured Streaming). Phase B of ADR-0045. Same shape as
# services/audit-sink/Dockerfile and services/ai-sink/Dockerfile:
# distroless static, nonroot, BuildKit module/build caches.
#
# Build context MUST be the openfoundry-go monorepo root:
#   docker build -f services/action-log-sink/Dockerfile \
#       --build-arg VERSION=$(git describe --tags --always) \
#       --build-arg VCS_REF=$(git rev-parse HEAD) \
#       --build-arg BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
#       -t openfoundry/action-log-sink:$VERSION .

ARG GO_VERSION=1.26.3
ARG ALPINE_VERSION=3.22
ARG DISTROLESS_IMAGE=gcr.io/distroless/static-debian12:nonroot

############################################################
# Stage 1 — modules
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
# Stage 2 — builder
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
            -o /out/action-log-sink \
            ./services/action-log-sink/cmd/action-log-sink

############################################################
# Stage 3 — runtime
############################################################
FROM ${DISTROLESS_IMAGE} AS runtime

ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.title="openfoundry-action-log-sink" \
      org.opencontainers.image.description="Kafka ontology.actions.applied.v1 -> Iceberg lakekeeper.default.action_log. Go port of ActionLogStreamSink." \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="https://github.com/openfoundry/openfoundry-go" \
      org.opencontainers.image.vendor="OpenFoundry" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.base.name="gcr.io/distroless/static-debian12:nonroot"

COPY --from=builder /out/action-log-sink /usr/local/bin/action-log-sink

USER nonroot:nonroot
WORKDIR /home/nonroot
STOPSIGNAL SIGTERM

EXPOSE 9090

ENTRYPOINT ["/usr/local/bin/action-log-sink"]
