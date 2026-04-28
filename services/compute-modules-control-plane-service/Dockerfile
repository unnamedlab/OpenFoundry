FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p compute-modules-control-plane-service && strip /workspace/target/release/compute-modules-control-plane-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50148

COPY --from=builder /workspace/target/release/compute-modules-control-plane-service /usr/local/bin/compute-modules-control-plane-service

EXPOSE 50148
CMD ["/usr/local/bin/compute-modules-control-plane-service"]
