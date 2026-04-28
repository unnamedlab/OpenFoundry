FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p execution-observability-service && strip /workspace/target/release/execution-observability-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50152

COPY --from=builder /workspace/target/release/execution-observability-service /usr/local/bin/execution-observability-service

EXPOSE 50152
CMD ["/usr/local/bin/execution-observability-service"]
