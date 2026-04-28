FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p cipher-service && strip /workspace/target/release/cipher-service

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/cipher-service

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50073

COPY --from=builder /workspace/target/release/cipher-service /usr/local/bin/cipher-service
COPY --from=builder /workspace/services/cipher-service/config ./config

EXPOSE 50073
CMD ["/usr/local/bin/cipher-service"]

