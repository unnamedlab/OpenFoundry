FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p llm-catalog-service && strip /workspace/target/release/llm-catalog-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50095

COPY --from=builder /workspace/target/release/llm-catalog-service /usr/local/bin/llm-catalog-service
COPY --from=builder /workspace/services/llm-catalog-service/config ./config

EXPOSE 50095
CMD ["/usr/local/bin/llm-catalog-service"]
