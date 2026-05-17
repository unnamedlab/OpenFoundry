# syntax=docker/dockerfile:1.10
#
# iceberg-object-indexer — Go port of the Scala IcebergToObjectStoreIndexer.
# Reads an Iceberg table via apache/iceberg-go (Lakekeeper REST catalog)
# and PUTs each row into object-database-service. Replaces the
# `pipeline-runner-spark` SparkApplication CR pattern with a plain
# distroless Job image.
#
# Shape mirrors services/template/Dockerfile so platform tooling (BuildKit
# module/build caches, SBOM labels, distroless static, nonroot user)
# applies uniformly.
#
# Build context MUST be the openfoundry-go monorepo root:
#   docker build -f services/iceberg-object-indexer/Dockerfile \
#       --build-arg VERSION=$(git describe --tags --always) \
#       --build-arg VCS_REF=$(git rev-parse HEAD) \
#       --build-arg BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
#       -t openfoundry/iceberg-object-indexer:$VERSION .

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
# Stage 2 — builder: compiles the indexer binary.
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
            -o /out/iceberg-object-indexer \
            ./services/iceberg-object-indexer/cmd/iceberg-object-indexer

############################################################
# Stage 3 — runtime: minimal, immutable, nonroot.
############################################################
FROM ${DISTROLESS_IMAGE} AS runtime

ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.title="openfoundry-iceberg-object-indexer" \
      org.opencontainers.image.description="Reads Iceberg rows and writes them to object-database-service. Go port of the Scala IcebergToObjectStoreIndexer." \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="https://github.com/openfoundry/openfoundry-go" \
      org.opencontainers.image.vendor="OpenFoundry" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.base.name="gcr.io/distroless/static-debian12:nonroot"

COPY --from=builder /out/iceberg-object-indexer /usr/local/bin/iceberg-object-indexer

USER nonroot:nonroot
WORKDIR /home/nonroot
STOPSIGNAL SIGTERM

EXPOSE 9090

ENTRYPOINT ["/usr/local/bin/iceberg-object-indexer"]
