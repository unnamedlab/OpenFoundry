FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p dataset-versioning-service && strip /workspace/target/release/dataset-versioning-service

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/dataset-versioning-service

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50078

COPY --from=builder /workspace/target/release/dataset-versioning-service /usr/local/bin/dataset-versioning-service
COPY --from=builder /workspace/services/dataset-versioning-service/config ./config

EXPOSE 50078
CMD ["/usr/local/bin/dataset-versioning-service"]
