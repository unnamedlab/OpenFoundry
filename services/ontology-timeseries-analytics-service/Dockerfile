FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p ontology-timeseries-analytics-service && strip /workspace/target/release/ontology-timeseries-analytics-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50132

COPY --from=builder /workspace/target/release/ontology-timeseries-analytics-service /usr/local/bin/ontology-timeseries-analytics-service

EXPOSE 50132
CMD ["/usr/local/bin/ontology-timeseries-analytics-service"]
