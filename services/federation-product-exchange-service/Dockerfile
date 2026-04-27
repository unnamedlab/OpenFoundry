FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p federation-product-exchange-service

FROM debian:bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50120

COPY --from=builder /workspace/target/release/federation-product-exchange-service /usr/local/bin/federation-product-exchange-service
COPY --from=builder /workspace/services/federation-product-exchange-service/config ./config

EXPOSE 50120
CMD ["/usr/local/bin/federation-product-exchange-service"]
