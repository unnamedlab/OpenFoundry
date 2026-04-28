FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p retention-policy-service && strip /workspace/target/release/retention-policy-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50117

COPY --from=builder /workspace/target/release/retention-policy-service /usr/local/bin/retention-policy-service
COPY --from=builder /workspace/services/retention-policy-service/config ./config

EXPOSE 50117
CMD ["/usr/local/bin/retention-policy-service"]
