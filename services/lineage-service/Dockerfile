FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev python3-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p lineage-service

FROM debian:bookworm-slim AS runtime
WORKDIR /workspace/services/lineage-service

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 python3-minimal \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50083

COPY --from=builder /workspace/target/release/lineage-service /usr/local/bin/lineage-service
COPY --from=builder /workspace/services/lineage-service/config ./config

EXPOSE 50083
CMD ["/usr/local/bin/lineage-service"]
