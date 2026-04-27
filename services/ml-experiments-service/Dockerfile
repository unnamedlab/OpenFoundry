FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p ml-experiments-service

FROM debian:bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50084

COPY --from=builder /workspace/target/release/ml-experiments-service /usr/local/bin/ml-experiments-service
COPY --from=builder /workspace/services/ml-experiments-service/config ./config

EXPOSE 50084
CMD ["/usr/local/bin/ml-experiments-service"]
