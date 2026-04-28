FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p ai-service && strip /workspace/target/release/ai-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50060

COPY --from=builder /workspace/target/release/ai-service /usr/local/bin/ai-service
COPY --from=builder /workspace/services/ai-service/config ./config

EXPOSE 50060
CMD ["/usr/local/bin/ai-service"]
