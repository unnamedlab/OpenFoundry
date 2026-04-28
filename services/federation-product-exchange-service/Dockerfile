FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p federation-product-exchange-service && strip /workspace/target/release/federation-product-exchange-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50120

COPY --from=builder /workspace/target/release/federation-product-exchange-service /usr/local/bin/federation-product-exchange-service
COPY --from=builder /workspace/services/federation-product-exchange-service/config ./config

EXPOSE 50120
CMD ["/usr/local/bin/federation-product-exchange-service"]
