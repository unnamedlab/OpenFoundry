FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p notification-service

FROM debian:bookworm-slim AS runtime
WORKDIR /workspace/services/notification-service

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50069

COPY --from=builder /workspace/target/release/notification-service /usr/local/bin/notification-service
COPY --from=builder /workspace/services/notification-service/config ./config

EXPOSE 50069
CMD ["/usr/local/bin/notification-service"]
