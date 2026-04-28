FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p managed-workspace-service && strip /workspace/target/release/managed-workspace-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50145

COPY --from=builder /workspace/target/release/managed-workspace-service /usr/local/bin/managed-workspace-service

EXPOSE 50145
CMD ["/usr/local/bin/managed-workspace-service"]
