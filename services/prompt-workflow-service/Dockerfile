FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p prompt-workflow-service && strip /workspace/target/release/prompt-workflow-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50096

COPY --from=builder /workspace/target/release/prompt-workflow-service /usr/local/bin/prompt-workflow-service
COPY --from=builder /workspace/services/prompt-workflow-service/config ./config

EXPOSE 50096
CMD ["/usr/local/bin/prompt-workflow-service"]
