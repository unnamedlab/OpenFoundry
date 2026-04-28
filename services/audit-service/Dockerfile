FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p audit-service && strip /workspace/target/release/audit-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50070

COPY --from=builder /workspace/target/release/audit-service /usr/local/bin/audit-service
COPY --from=builder /workspace/services/audit-service/config ./config

EXPOSE 50070
CMD ["/usr/local/bin/audit-service"]
