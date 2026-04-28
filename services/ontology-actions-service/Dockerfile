FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev python3-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p ontology-actions-service && strip /workspace/target/release/ontology-actions-service

FROM node:24.15.0-alpine3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3 python3

ENV HOST=0.0.0.0
ENV PORT=50106
ENV NODE_RUNTIME_COMMAND=node

COPY --from=builder /workspace/target/release/ontology-actions-service /usr/local/bin/ontology-actions-service

EXPOSE 50106
CMD ["/usr/local/bin/ontology-actions-service"]
