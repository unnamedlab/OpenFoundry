FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p virtual-table-service && strip /workspace/target/release/virtual-table-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50089

COPY --from=builder /workspace/target/release/virtual-table-service /usr/local/bin/virtual-table-service

EXPOSE 50089
CMD ["/usr/local/bin/virtual-table-service"]
