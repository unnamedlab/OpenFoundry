FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p dataset-service && strip /workspace/target/release/dataset-service

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/dataset-service

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50053

COPY --from=builder /workspace/target/release/dataset-service /usr/local/bin/dataset-service
COPY --from=builder /workspace/services/dataset-service/config ./config

EXPOSE 50053
CMD ["/usr/local/bin/dataset-service"]
