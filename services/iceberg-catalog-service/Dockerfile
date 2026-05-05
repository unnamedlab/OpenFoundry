# syntax=docker/dockerfile:1.7
# iceberg-catalog-service — Foundry Iceberg REST Catalog [Beta].

FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates python3
ENV PYO3_PYTHON=python3
ENV PYO3_USE_ABI3_FORWARD_COMPATIBILITY=1

COPY . .
RUN --mount=type=cache,target=/usr/local/cargo/registry,sharing=locked \
    --mount=type=cache,target=/usr/local/cargo/git,sharing=locked \
    --mount=type=cache,target=/workspace/target,id=iceberg-catalog-service-target,sharing=locked \
    cargo build --locked --profile docker-release -p iceberg-catalog-service \
    && cp /workspace/target/docker-release/iceberg-catalog-service /tmp/iceberg-catalog-service \
    && strip /tmp/iceberg-catalog-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV ICEBERG_CATALOG_HOST=0.0.0.0
ENV ICEBERG_CATALOG_PORT=8197

COPY --from=builder /tmp/iceberg-catalog-service /usr/local/bin/iceberg-catalog-service
COPY --from=builder /workspace/services/iceberg-catalog-service/config ./config
EXPOSE 8197
CMD ["/usr/local/bin/iceberg-catalog-service"]
