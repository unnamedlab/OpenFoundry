FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p connector-management-service && strip /workspace/target/release/connector-management-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50088

COPY --from=builder /workspace/target/release/connector-management-service /usr/local/bin/connector-management-service

EXPOSE 50088
CMD ["/usr/local/bin/connector-management-service"]
