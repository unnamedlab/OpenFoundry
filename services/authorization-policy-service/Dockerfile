FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p authorization-policy-service && strip /workspace/target/release/authorization-policy-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50093

COPY --from=builder /workspace/target/release/authorization-policy-service /usr/local/bin/authorization-policy-service
COPY --from=builder /workspace/services/authorization-policy-service/config ./config

EXPOSE 50093
CMD ["/usr/local/bin/authorization-policy-service"]
