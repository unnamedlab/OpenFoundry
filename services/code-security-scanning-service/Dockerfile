FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p code-security-scanning-service && strip /workspace/target/release/code-security-scanning-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50154

COPY --from=builder /workspace/target/release/code-security-scanning-service /usr/local/bin/code-security-scanning-service

EXPOSE 50154
CMD ["/usr/local/bin/code-security-scanning-service"]
