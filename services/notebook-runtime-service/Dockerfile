FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p notebook-runtime-service && strip /workspace/target/release/notebook-runtime-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50134

COPY --from=builder /workspace/target/release/notebook-runtime-service /usr/local/bin/notebook-runtime-service

EXPOSE 50134
CMD ["/usr/local/bin/notebook-runtime-service"]
