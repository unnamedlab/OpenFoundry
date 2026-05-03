# syntax=docker/dockerfile:1.7
FROM golang:1.23-alpine AS builder
WORKDIR /workspace
RUN apk add --no-cache git ca-certificates
COPY workers-go/ ./workers-go/
WORKDIR /workspace/workers-go/approvals
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /tmp/approvals-worker .

FROM alpine:3.23 AS runtime
WORKDIR /app
RUN apk add --no-cache ca-certificates && \
    addgroup -S worker && adduser -S worker -G worker -u 10001
COPY --from=builder /tmp/approvals-worker /usr/local/bin/approvals-worker
ENV TEMPORAL_ADDRESS=temporal-frontend.temporal.svc.cluster.local:7233
ENV TEMPORAL_HOST_PORT=temporal-frontend.temporal.svc.cluster.local:7233
ENV TEMPORAL_NAMESPACE=default
ENV TEMPORAL_TASK_QUEUE=openfoundry.approvals
ENV OF_LOG_LEVEL=info
ENV METRICS_ADDR=:9090
USER worker
EXPOSE 9090
CMD ["/usr/local/bin/approvals-worker"]
