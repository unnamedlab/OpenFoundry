FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev python3-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p object-database-service && strip /workspace/target/release/object-database-service

FROM node:24.15.0-alpine3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3 python3

ENV HOST=0.0.0.0
ENV PORT=50104
ENV NODE_RUNTIME_COMMAND=node

COPY --from=builder /workspace/target/release/object-database-service /usr/local/bin/object-database-service

EXPOSE 50104
CMD ["/usr/local/bin/object-database-service"]
