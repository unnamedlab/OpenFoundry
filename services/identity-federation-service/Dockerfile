FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p identity-federation-service

FROM debian:bookworm-slim AS runtime
WORKDIR /workspace/services/identity-federation-service

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50051

COPY --from=builder /workspace/target/release/identity-federation-service /usr/local/bin/identity-federation-service
COPY --from=builder /workspace/services/identity-federation-service/config ./config

EXPOSE 50112
CMD ["/usr/local/bin/identity-federation-service"]
