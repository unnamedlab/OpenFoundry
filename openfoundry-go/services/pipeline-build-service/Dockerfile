# syntax=docker/dockerfile:1.7
FROM golang:1.25 AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/pipeline-build-service \
      ./services/pipeline-build-service/cmd/pipeline-build-service

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/pipeline-build-service /usr/local/bin/service
COPY --from=builder /workspace/services/pipeline-build-service/config.yaml /etc/openfoundry/config.yaml
USER nonroot:nonroot
EXPOSE 50081
ENTRYPOINT ["/usr/local/bin/service"]
