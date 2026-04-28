FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p model-lifecycle-service && strip /workspace/target/release/model-lifecycle-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50126

COPY --from=builder /workspace/target/release/model-lifecycle-service /usr/local/bin/model-lifecycle-service

EXPOSE 50126
CMD ["/usr/local/bin/model-lifecycle-service"]
