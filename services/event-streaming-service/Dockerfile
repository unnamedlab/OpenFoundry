FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p event-streaming-service && strip /workspace/target/release/event-streaming-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50121

COPY --from=builder /workspace/target/release/event-streaming-service /usr/local/bin/event-streaming-service

EXPOSE 50121
CMD ["/usr/local/bin/event-streaming-service"]
