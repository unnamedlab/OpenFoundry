FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p security-governance-service && strip /workspace/target/release/security-governance-service

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/security-governance-service

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50114

COPY --from=builder /workspace/target/release/security-governance-service /usr/local/bin/security-governance-service
COPY --from=builder /workspace/services/security-governance-service/config ./config

EXPOSE 50114
CMD ["/usr/local/bin/security-governance-service"]
