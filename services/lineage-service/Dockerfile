FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev python3-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p lineage-service && strip /workspace/target/release/lineage-service

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/lineage-service

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3 python3

ENV HOST=0.0.0.0
ENV PORT=50083

COPY --from=builder /workspace/target/release/lineage-service /usr/local/bin/lineage-service
COPY --from=builder /workspace/services/lineage-service/config ./config

EXPOSE 50083
CMD ["/usr/local/bin/lineage-service"]
