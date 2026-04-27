FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p session-governance-service

FROM debian:bookworm-slim AS runtime
WORKDIR /workspace/services/session-governance-service

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50074

COPY --from=builder /workspace/target/release/session-governance-service /usr/local/bin/session-governance-service
COPY --from=builder /workspace/services/session-governance-service/config ./config

EXPOSE 50074
CMD ["/usr/local/bin/session-governance-service"]

