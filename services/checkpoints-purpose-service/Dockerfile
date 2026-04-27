FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p checkpoints-purpose-service

FROM debian:bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50116

COPY --from=builder /workspace/target/release/checkpoints-purpose-service /usr/local/bin/checkpoints-purpose-service
COPY --from=builder /workspace/services/checkpoints-purpose-service/config ./config

EXPOSE 50116
CMD ["/usr/local/bin/checkpoints-purpose-service"]
