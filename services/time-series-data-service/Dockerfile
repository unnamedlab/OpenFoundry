FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p time-series-data-service && strip /workspace/target/release/time-series-data-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50124

COPY --from=builder /workspace/target/release/time-series-data-service /usr/local/bin/time-series-data-service

EXPOSE 50124
CMD ["/usr/local/bin/time-series-data-service"]
