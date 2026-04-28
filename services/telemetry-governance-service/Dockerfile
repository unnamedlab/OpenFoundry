FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p telemetry-governance-service && strip /workspace/target/release/telemetry-governance-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50153

COPY --from=builder /workspace/target/release/telemetry-governance-service /usr/local/bin/telemetry-governance-service

EXPOSE 50153
CMD ["/usr/local/bin/telemetry-governance-service"]
