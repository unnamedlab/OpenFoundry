FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p data-asset-catalog-service && strip /workspace/target/release/data-asset-catalog-service

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/data-asset-catalog-service

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50079

COPY --from=builder /workspace/target/release/data-asset-catalog-service /usr/local/bin/data-asset-catalog-service
COPY --from=builder /workspace/services/data-asset-catalog-service/config ./config

EXPOSE 50079
CMD ["/usr/local/bin/data-asset-catalog-service"]
