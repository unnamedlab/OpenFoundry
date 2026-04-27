FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p oauth-integration-service

FROM debian:bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50094

COPY --from=builder /workspace/target/release/oauth-integration-service /usr/local/bin/oauth-integration-service
COPY --from=builder /workspace/services/oauth-integration-service/config ./config

EXPOSE 50094
CMD ["/usr/local/bin/oauth-integration-service"]
