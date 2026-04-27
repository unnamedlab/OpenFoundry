FROM rust:1.85-bookworm AS builder
WORKDIR /workspace

RUN apt-get update \
    && apt-get install -y --no-install-recommends pkg-config libssl-dev ca-certificates git \
    && rm -rf /var/lib/apt/lists/*

COPY . .
RUN cargo build --locked --release -p code-repo-service

FROM debian:bookworm-slim AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates libssl3 git \
    && rm -rf /var/lib/apt/lists/*

ENV HOST=0.0.0.0
ENV PORT=50065

COPY --from=builder /workspace/target/release/code-repo-service /usr/local/bin/code-repo-service

EXPOSE 50065
CMD ["/usr/local/bin/code-repo-service"]
