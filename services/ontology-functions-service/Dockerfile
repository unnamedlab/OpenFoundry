FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev python3-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p ontology-functions-service

FROM node:22-bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 python3-minimal \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50108
ENV NODE_RUNTIME_COMMAND=node

COPY --from=builder /workspace/target/release/ontology-functions-service /usr/local/bin/ontology-functions-service

EXPOSE 50108
CMD ["/usr/local/bin/ontology-functions-service"]
