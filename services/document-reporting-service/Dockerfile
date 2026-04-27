FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p document-reporting-service

FROM debian:bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50102

COPY --from=builder /workspace/target/release/document-reporting-service /usr/local/bin/document-reporting-service
COPY --from=builder /workspace/services/document-reporting-service/config ./config

EXPOSE 50102
CMD ["/usr/local/bin/document-reporting-service"]
