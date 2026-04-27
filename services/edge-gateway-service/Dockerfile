FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p edge-gateway-service

FROM debian:bookworm-slim AS runtime
WORKDIR /workspace/services/edge-gateway-service

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=8080

COPY --from=builder /workspace/target/release/edge-gateway-service /usr/local/bin/edge-gateway-service
COPY --from=builder /workspace/services/edge-gateway-service/config ./config

EXPOSE 8080
CMD ["/usr/local/bin/edge-gateway-service"]
