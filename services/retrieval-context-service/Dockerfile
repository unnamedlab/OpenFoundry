FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p retrieval-context-service && strip /workspace/target/release/retrieval-context-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50098

COPY --from=builder /workspace/target/release/retrieval-context-service /usr/local/bin/retrieval-context-service
COPY --from=builder /workspace/services/retrieval-context-service/config ./config

EXPOSE 50098
CMD ["/usr/local/bin/retrieval-context-service"]
