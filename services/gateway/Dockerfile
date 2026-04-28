FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p gateway && strip /workspace/target/release/gateway

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/gateway

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=8080

COPY --from=builder /workspace/target/release/gateway /usr/local/bin/gateway
COPY --from=builder /workspace/services/gateway/config ./config

EXPOSE 8080
CMD ["/usr/local/bin/gateway"]
