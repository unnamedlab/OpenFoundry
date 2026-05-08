# syntax=docker/dockerfile:1.10
#
# OpenFoundry Go service — multi-stage, distroless static, nonroot.
#
# Cache strategy
#   1. Module graph (`go.mod` / `go.sum`) is downloaded in an isolated
#      stage that only invalidates when the dependency graph changes.
#      All 41 services share the same module cache via BuildKit
#      `--mount=type=cache,sharing=locked` — the first service warms
#      the cache, every subsequent build reuses it.
#   2. The source tree is mounted at build time (`--mount=type=bind`)
#      instead of being COPY-ed, so source-only changes never bloat the
#      builder image and only the actual `go build` step rehashes.
#   3. Compiled artefacts cache lives in `/root/.cache/go-build`,
#      mounted as a sticky BuildKit cache for incremental rebuilds.
#
# Final image
#   * Base: `gcr.io/distroless/static-debian12:nonroot` (~2 MB).
#     No shell, no package manager, no setuid binaries, no apt sources.
#   * Binary: pure-Go static (CGO disabled, `osusergo,netgo`),
#     stripped (`-s -w -buildid=`), trimmed (`-trimpath`), reproducible.
#   * Runs as `nonroot` (uid 65532). `STOPSIGNAL SIGTERM` for graceful
#     shutdown under k8s. OCI image labels populated for SBOM/registry.
#
# Build context MUST be the openfoundry-go monorepo root:
#   docker build -f services/solution-design-service/Dockerfile \
#       --build-arg VERSION=$(git describe --tags --always) \
#       --build-arg VCS_REF=$(git rev-parse HEAD) \
#       --build-arg BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
#       -t openfoundry/solution-design-service:$VERSION .

ARG GO_VERSION=1.26.3
ARG ALPINE_VERSION=3.22
ARG DISTROLESS_IMAGE=gcr.io/distroless/static-debian12:nonroot

############################################################
# Stage 1 — modules: pre-warmed Go module graph.
#           Re-runs only when go.mod / go.sum change.
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
# Stage 2 — builder: compiles the solution-design-service binary.
#           Source tree is bind-mounted (no fat COPY layer).
############################################################
FROM modules AS builder

ARG SERVICE_NAME=solution-design-service
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
            -o /out/service \
            ./services/${SERVICE_NAME}/cmd/${SERVICE_NAME}

############################################################
# Stage 3 — runtime: minimal, immutable, nonroot.
############################################################
FROM ${DISTROLESS_IMAGE} AS runtime

ARG SERVICE_NAME=solution-design-service
ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.title="openfoundry-solution-design-service" \
      org.opencontainers.image.description="OpenFoundry solution-design-service microservice" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="https://github.com/openfoundry/openfoundry-go" \
      org.opencontainers.image.vendor="OpenFoundry" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.base.name="gcr.io/distroless/static-debian12:nonroot"

COPY --from=builder /out/service /usr/local/bin/service

USER nonroot:nonroot
WORKDIR /home/nonroot
STOPSIGNAL SIGTERM

EXPOSE 50142

ENTRYPOINT ["/usr/local/bin/service"]
