FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p compute-modules-control-plane-service

FROM debian:bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50148

COPY --from=builder /workspace/target/release/compute-modules-control-plane-service /usr/local/bin/compute-modules-control-plane-service

EXPOSE 50148
CMD ["/usr/local/bin/compute-modules-control-plane-service"]
