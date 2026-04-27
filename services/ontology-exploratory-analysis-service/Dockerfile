FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p ontology-exploratory-analysis-service

FROM debian:bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50131

COPY --from=builder /workspace/target/release/ontology-exploratory-analysis-service /usr/local/bin/ontology-exploratory-analysis-service

EXPOSE 50131
CMD ["/usr/local/bin/ontology-exploratory-analysis-service"]
