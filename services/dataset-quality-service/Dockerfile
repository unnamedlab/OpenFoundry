FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p dataset-quality-service && strip /workspace/target/release/dataset-quality-service

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/dataset-quality-service

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50072

COPY --from=builder /workspace/target/release/dataset-quality-service /usr/local/bin/dataset-quality-service
COPY --from=builder /workspace/services/dataset-quality-service/config ./config

EXPOSE 50072
CMD ["/usr/local/bin/dataset-quality-service"]

