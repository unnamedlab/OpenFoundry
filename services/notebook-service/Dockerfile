FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev python3-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p notebook-service && strip /workspace/target/release/notebook-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3 python3

ENV HOST=0.0.0.0
ENV PORT=50062

COPY --from=builder /workspace/target/release/notebook-service /usr/local/bin/notebook-service

EXPOSE 50062
CMD ["/usr/local/bin/notebook-service"]
