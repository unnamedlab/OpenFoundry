FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p marketplace-service && strip /workspace/target/release/marketplace-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50066

COPY --from=builder /workspace/target/release/marketplace-service /usr/local/bin/marketplace-service
COPY --from=builder /workspace/services/marketplace-service/config ./config

EXPOSE 50066
CMD ["/usr/local/bin/marketplace-service"]
