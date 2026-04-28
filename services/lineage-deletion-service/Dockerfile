FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p lineage-deletion-service && strip /workspace/target/release/lineage-deletion-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50118

COPY --from=builder /workspace/target/release/lineage-deletion-service /usr/local/bin/lineage-deletion-service
COPY --from=builder /workspace/services/lineage-deletion-service/config ./config

EXPOSE 50118
CMD ["/usr/local/bin/lineage-deletion-service"]
