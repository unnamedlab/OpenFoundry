FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p dataset-service

FROM debian:bookworm-slim AS runtime
WORKDIR /workspace/services/dataset-service

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50053

COPY --from=builder /workspace/target/release/dataset-service /usr/local/bin/dataset-service
COPY --from=builder /workspace/services/dataset-service/config ./config

EXPOSE 50053
CMD ["/usr/local/bin/dataset-service"]
