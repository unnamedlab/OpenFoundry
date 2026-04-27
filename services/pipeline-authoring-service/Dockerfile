FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev python3-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p pipeline-authoring-service

FROM debian:bookworm-slim AS runtime
WORKDIR /workspace/services/pipeline-authoring-service

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 python3-minimal \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50080

COPY --from=builder /workspace/target/release/pipeline-authoring-service /usr/local/bin/pipeline-authoring-service
COPY --from=builder /workspace/services/pipeline-authoring-service/config ./config

EXPOSE 50080
CMD ["/usr/local/bin/pipeline-authoring-service"]
