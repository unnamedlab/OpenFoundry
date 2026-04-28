FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p automation-operations-service && strip /workspace/target/release/automation-operations-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50138

COPY --from=builder /workspace/target/release/automation-operations-service /usr/local/bin/automation-operations-service

EXPOSE 50138
CMD ["/usr/local/bin/automation-operations-service"]
