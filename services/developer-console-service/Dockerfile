FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p developer-console-service && strip /workspace/target/release/developer-console-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50143

COPY --from=builder /workspace/target/release/developer-console-service /usr/local/bin/developer-console-service

EXPOSE 50143
CMD ["/usr/local/bin/developer-console-service"]
