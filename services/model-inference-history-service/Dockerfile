FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p model-inference-history-service && strip /workspace/target/release/model-inference-history-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50092

COPY --from=builder /workspace/target/release/model-inference-history-service /usr/local/bin/model-inference-history-service
COPY --from=builder /workspace/services/model-inference-history-service/config ./config

EXPOSE 50092
CMD ["/usr/local/bin/model-inference-history-service"]
