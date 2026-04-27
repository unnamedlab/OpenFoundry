FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p dataset-quality-service

FROM debian:bookworm-slim AS runtime
WORKDIR /workspace/services/dataset-quality-service

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50072

COPY --from=builder /workspace/target/release/dataset-quality-service /usr/local/bin/dataset-quality-service
COPY --from=builder /workspace/services/dataset-quality-service/config ./config

EXPOSE 50072
CMD ["/usr/local/bin/dataset-quality-service"]

