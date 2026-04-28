FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p monitoring-rules-service && strip /workspace/target/release/monitoring-rules-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50150

COPY --from=builder /workspace/target/release/monitoring-rules-service /usr/local/bin/monitoring-rules-service

EXPOSE 50150
CMD ["/usr/local/bin/monitoring-rules-service"]
