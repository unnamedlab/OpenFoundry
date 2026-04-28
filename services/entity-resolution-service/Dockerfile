FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p entity-resolution-service && strip /workspace/target/release/entity-resolution-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50058

COPY --from=builder /workspace/target/release/entity-resolution-service /usr/local/bin/entity-resolution-service

EXPOSE 50058
CMD ["/usr/local/bin/entity-resolution-service"]
