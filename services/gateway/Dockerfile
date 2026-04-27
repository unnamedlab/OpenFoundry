FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p gateway

FROM debian:bookworm-slim AS runtime
WORKDIR /workspace/services/gateway

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=8080

COPY --from=builder /workspace/target/release/gateway /usr/local/bin/gateway
COPY --from=builder /workspace/services/gateway/config ./config

EXPOSE 8080
CMD ["/usr/local/bin/gateway"]
