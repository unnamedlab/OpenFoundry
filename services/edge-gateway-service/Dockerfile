FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p edge-gateway-service && strip /workspace/target/release/edge-gateway-service

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/edge-gateway-service

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=8080

COPY --from=builder /workspace/target/release/edge-gateway-service /usr/local/bin/edge-gateway-service
COPY --from=builder /workspace/services/edge-gateway-service/config ./config

EXPOSE 8080
CMD ["/usr/local/bin/edge-gateway-service"]
