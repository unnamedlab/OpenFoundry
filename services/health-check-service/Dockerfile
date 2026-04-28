FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p health-check-service && strip /workspace/target/release/health-check-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50151

COPY --from=builder /workspace/target/release/health-check-service /usr/local/bin/health-check-service

EXPOSE 50151
CMD ["/usr/local/bin/health-check-service"]
