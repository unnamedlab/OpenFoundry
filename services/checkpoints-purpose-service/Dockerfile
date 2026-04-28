FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p checkpoints-purpose-service && strip /workspace/target/release/checkpoints-purpose-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50116

COPY --from=builder /workspace/target/release/checkpoints-purpose-service /usr/local/bin/checkpoints-purpose-service
COPY --from=builder /workspace/services/checkpoints-purpose-service/config ./config

EXPOSE 50116
CMD ["/usr/local/bin/checkpoints-purpose-service"]
