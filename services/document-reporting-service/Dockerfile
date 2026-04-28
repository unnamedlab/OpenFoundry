FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p document-reporting-service && strip /workspace/target/release/document-reporting-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50102

COPY --from=builder /workspace/target/release/document-reporting-service /usr/local/bin/document-reporting-service
COPY --from=builder /workspace/services/document-reporting-service/config ./config

EXPOSE 50102
CMD ["/usr/local/bin/document-reporting-service"]
