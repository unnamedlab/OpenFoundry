FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p retrieval-context-service

FROM debian:bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50098

COPY --from=builder /workspace/target/release/retrieval-context-service /usr/local/bin/retrieval-context-service
COPY --from=builder /workspace/services/retrieval-context-service/config ./config

EXPOSE 50098
CMD ["/usr/local/bin/retrieval-context-service"]
