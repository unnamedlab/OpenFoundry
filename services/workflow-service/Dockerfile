FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p workflow-service && strip /workspace/target/release/workflow-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50061

COPY --from=builder /workspace/target/release/workflow-service /usr/local/bin/workflow-service
COPY --from=builder /workspace/services/workflow-service/config ./config

EXPOSE 50061
CMD ["/usr/local/bin/workflow-service"]
