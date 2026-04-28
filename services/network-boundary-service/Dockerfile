FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p network-boundary-service && strip /workspace/target/release/network-boundary-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50119

COPY --from=builder /workspace/target/release/network-boundary-service /usr/local/bin/network-boundary-service
COPY --from=builder /workspace/services/network-boundary-service/config ./config

EXPOSE 50119
CMD ["/usr/local/bin/network-boundary-service"]
