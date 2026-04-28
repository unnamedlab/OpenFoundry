FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p nexus-service && strip /workspace/target/release/nexus-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50067

COPY --from=builder /workspace/target/release/nexus-service /usr/local/bin/nexus-service
COPY --from=builder /workspace/services/nexus-service/config ./config

EXPOSE 50067
CMD ["/usr/local/bin/nexus-service"]
