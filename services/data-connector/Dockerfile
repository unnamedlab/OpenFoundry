FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p data-connector && strip /workspace/target/release/data-connector

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50052

COPY --from=builder /workspace/target/release/data-connector /usr/local/bin/data-connector

EXPOSE 50052
CMD ["/usr/local/bin/data-connector"]
