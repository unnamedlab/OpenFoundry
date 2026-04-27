FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p network-boundary-service

FROM debian:bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50119

COPY --from=builder /workspace/target/release/network-boundary-service /usr/local/bin/network-boundary-service
COPY --from=builder /workspace/services/network-boundary-service/config ./config

EXPOSE 50119
CMD ["/usr/local/bin/network-boundary-service"]
