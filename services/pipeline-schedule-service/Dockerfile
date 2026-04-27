FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev python3-dev ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p pipeline-schedule-service

FROM debian:bookworm-slim AS runtime
WORKDIR /workspace/services/pipeline-schedule-service

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 python3-minimal \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50082

COPY --from=builder /workspace/target/release/pipeline-schedule-service /usr/local/bin/pipeline-schedule-service
COPY --from=builder /workspace/services/pipeline-schedule-service/config ./config

EXPOSE 50082
CMD ["/usr/local/bin/pipeline-schedule-service"]
