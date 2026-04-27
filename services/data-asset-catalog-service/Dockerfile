FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p data-asset-catalog-service

FROM debian:bookworm-slim AS runtime
WORKDIR /workspace/services/data-asset-catalog-service

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50079

COPY --from=builder /workspace/target/release/data-asset-catalog-service /usr/local/bin/data-asset-catalog-service
COPY --from=builder /workspace/services/data-asset-catalog-service/config ./config

EXPOSE 50079
CMD ["/usr/local/bin/data-asset-catalog-service"]
