FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p approvals-service && strip /workspace/target/release/approvals-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50071

COPY --from=builder /workspace/target/release/approvals-service /usr/local/bin/approvals-service
COPY --from=builder /workspace/services/approvals-service/config ./config

EXPOSE 50071
CMD ["/usr/local/bin/approvals-service"]
