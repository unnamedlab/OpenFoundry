FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p sql-bi-gateway-service && strip /workspace/target/release/sql-bi-gateway-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50133

COPY --from=builder /workspace/target/release/sql-bi-gateway-service /usr/local/bin/sql-bi-gateway-service

EXPOSE 50133
CMD ["/usr/local/bin/sql-bi-gateway-service"]
