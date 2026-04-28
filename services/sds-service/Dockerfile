FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p sds-service && strip /workspace/target/release/sds-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50076

COPY --from=builder /workspace/target/release/sds-service /usr/local/bin/sds-service
COPY --from=builder /workspace/services/sds-service/config ./config

EXPOSE 50076
CMD ["/usr/local/bin/sds-service"]
