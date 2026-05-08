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
      -o /out/sql-bi-gateway-service \
      ./services/sql-bi-gateway-service/cmd/sql-bi-gateway-service

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/sql-bi-gateway-service /usr/local/bin/service
COPY --from=builder /workspace/services/sql-bi-gateway-service/config.yaml /etc/openfoundry/config.yaml
USER nonroot:nonroot
EXPOSE 50133 50134
ENTRYPOINT ["/usr/local/bin/service"]
