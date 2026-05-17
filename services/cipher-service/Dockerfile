# syntax=docker/dockerfile:1.10
#
# cipher-service — multi-stage, distroless static, nonroot.
# Mirrors services/template/Dockerfile; the only override is SERVICE_NAME.
#
# Build context MUST be the openfoundry-go monorepo root:
#   docker build -f services/cipher-service/Dockerfile \
#       --build-arg VERSION=$(git describe --tags --always) \
#       --build-arg VCS_REF=$(git rev-parse HEAD) \
#       --build-arg BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
#       -t openfoundry/cipher-service:$VERSION .

ARG GO_VERSION=1.26.3
ARG ALPINE_VERSION=3.22
ARG DISTROLESS_IMAGE=gcr.io/distroless/static-debian12:nonroot

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

FROM modules AS builder

ARG SERVICE_NAME=cipher-service
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
            ./services/${SERVICE_NAME}/cmd/${SERVICE_NAME}; \
    install -D -m 0444 services/cipher-service/config.yaml /out/etc/openfoundry/config.yaml

FROM ${DISTROLESS_IMAGE} AS runtime

ARG SERVICE_NAME=cipher-service
ARG VERSION=dev
ARG VCS_REF=unknown
ARG BUILD_DATE=unknown

LABEL org.opencontainers.image.title="openfoundry-cipher-service" \
      org.opencontainers.image.description="OpenFoundry Cipher stub — 501 envelopes for /api/v1/auth/cipher" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="https://github.com/openfoundry/openfoundry-go" \
      org.opencontainers.image.vendor="OpenFoundry" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.base.name="gcr.io/distroless/static-debian12:nonroot"

COPY --from=builder /out/service /usr/local/bin/service
COPY --from=builder /out/etc/openfoundry/config.yaml /etc/openfoundry/config.yaml

USER nonroot:nonroot
WORKDIR /home/nonroot
STOPSIGNAL SIGTERM

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/service", "-config", "/etc/openfoundry/config.yaml"]
