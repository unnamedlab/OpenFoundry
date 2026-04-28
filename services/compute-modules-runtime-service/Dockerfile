FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p compute-modules-runtime-service && strip /workspace/target/release/compute-modules-runtime-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50149

COPY --from=builder /workspace/target/release/compute-modules-runtime-service /usr/local/bin/compute-modules-runtime-service

EXPOSE 50149
CMD ["/usr/local/bin/compute-modules-runtime-service"]
