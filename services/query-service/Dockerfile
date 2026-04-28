FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p query-service && strip /workspace/target/release/query-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50055

COPY --from=builder /workspace/target/release/query-service /usr/local/bin/query-service

EXPOSE 50055
CMD ["/usr/local/bin/query-service"]
