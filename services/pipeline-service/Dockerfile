FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev python3-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p pipeline-service

FROM debian:bookworm-slim AS runtime
WORKDIR /workspace/services/pipeline-service

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 python3-minimal \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50056

COPY --from=builder /workspace/target/release/pipeline-service /usr/local/bin/pipeline-service
COPY --from=builder /workspace/services/pipeline-service/config ./config

EXPOSE 50056
CMD ["/usr/local/bin/pipeline-service"]
