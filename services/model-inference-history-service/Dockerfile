FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p model-inference-history-service

FROM debian:bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50092

COPY --from=builder /workspace/target/release/model-inference-history-service /usr/local/bin/model-inference-history-service
COPY --from=builder /workspace/services/model-inference-history-service/config ./config

EXPOSE 50092
CMD ["/usr/local/bin/model-inference-history-service"]
