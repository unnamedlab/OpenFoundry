FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p conversation-state-service && strip /workspace/target/release/conversation-state-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50099

COPY --from=builder /workspace/target/release/conversation-state-service /usr/local/bin/conversation-state-service
COPY --from=builder /workspace/services/conversation-state-service/config ./config

EXPOSE 50099
CMD ["/usr/local/bin/conversation-state-service"]
