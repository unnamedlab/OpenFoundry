FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p ingestion-replication-service && strip /workspace/target/release/ingestion-replication-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50090

COPY --from=builder /workspace/target/release/ingestion-replication-service /usr/local/bin/ingestion-replication-service

EXPOSE 50090
CMD ["/usr/local/bin/ingestion-replication-service"]
