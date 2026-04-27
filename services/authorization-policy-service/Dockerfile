FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p authorization-policy-service

FROM debian:bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50093

COPY --from=builder /workspace/target/release/authorization-policy-service /usr/local/bin/authorization-policy-service
COPY --from=builder /workspace/services/authorization-policy-service/config ./config

EXPOSE 50093
CMD ["/usr/local/bin/authorization-policy-service"]
