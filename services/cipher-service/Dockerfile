FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p cipher-service

FROM debian:bookworm-slim AS runtime
WORKDIR /workspace/services/cipher-service

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50073

COPY --from=builder /workspace/target/release/cipher-service /usr/local/bin/cipher-service
COPY --from=builder /workspace/services/cipher-service/config ./config

EXPOSE 50073
CMD ["/usr/local/bin/cipher-service"]

