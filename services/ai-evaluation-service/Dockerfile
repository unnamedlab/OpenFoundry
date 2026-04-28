FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p ai-evaluation-service && strip /workspace/target/release/ai-evaluation-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50075

COPY --from=builder /workspace/target/release/ai-evaluation-service /usr/local/bin/ai-evaluation-service
COPY --from=builder /workspace/services/ai-evaluation-service/config ./config

EXPOSE 50075
CMD ["/usr/local/bin/ai-evaluation-service"]
