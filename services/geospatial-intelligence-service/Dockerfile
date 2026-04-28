FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p geospatial-intelligence-service && strip /workspace/target/release/geospatial-intelligence-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50068

COPY --from=builder /workspace/target/release/geospatial-intelligence-service /usr/local/bin/geospatial-intelligence-service

EXPOSE 50068
CMD ["/usr/local/bin/geospatial-intelligence-service"]
