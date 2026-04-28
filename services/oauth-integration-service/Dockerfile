FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p oauth-integration-service && strip /workspace/target/release/oauth-integration-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50094

COPY --from=builder /workspace/target/release/oauth-integration-service /usr/local/bin/oauth-integration-service
COPY --from=builder /workspace/services/oauth-integration-service/config ./config

EXPOSE 50094
CMD ["/usr/local/bin/oauth-integration-service"]
