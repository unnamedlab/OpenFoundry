FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p security-governance-service

FROM debian:bookworm-slim AS runtime
WORKDIR /workspace/services/security-governance-service

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50114

COPY --from=builder /workspace/target/release/security-governance-service /usr/local/bin/security-governance-service
COPY --from=builder /workspace/services/security-governance-service/config ./config

EXPOSE 50114
CMD ["/usr/local/bin/security-governance-service"]
