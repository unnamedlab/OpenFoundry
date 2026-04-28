FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p product-distribution-service && strip /workspace/target/release/product-distribution-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50111

COPY --from=builder /workspace/target/release/product-distribution-service /usr/local/bin/product-distribution-service

EXPOSE 50111
CMD ["/usr/local/bin/product-distribution-service"]
