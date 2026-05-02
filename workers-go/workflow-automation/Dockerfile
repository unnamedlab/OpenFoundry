# syntax=docker/dockerfile:1.7
# Multi-stage build for the Temporal worker `workflow-automation`.
# Substrate per S2.2.e — base image golang:1.23-alpine, single
# statically-linked binary in a scratch-style runtime.

FROM golang:1.23-alpine AS builder
WORKDIR /workspace

# Build deps for CGO-disabled static linking (default for Temporal
# SDK; we keep CGO off to avoid pulling glibc).
RUN apk add --no-cache git ca-certificates

# Copy the entire workers-go workspace so go.work resolves all
# replace directives. Build context for this Dockerfile must be the
# repo root (`docker build -f workers-go/workflow-automation/Dockerfile .`).
COPY workers-go/ ./workers-go/

WORKDIR /workspace/workers-go/workflow-automation
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /tmp/workflow-automation-worker \
        .

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates && \
    addgroup -S worker && adduser -S worker -G worker -u 10001

COPY --from=builder /tmp/workflow-automation-worker /usr/local/bin/workflow-automation-worker

ENV TEMPORAL_HOST_PORT=temporal-frontend.temporal.svc.cluster.local:7233
ENV TEMPORAL_NAMESPACE=default
ENV OF_LOG_LEVEL=info
ENV METRICS_ADDR=:9090

USER worker
EXPOSE 9090
CMD ["/usr/local/bin/workflow-automation-worker"]
