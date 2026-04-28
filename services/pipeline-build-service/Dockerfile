FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev python3-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p pipeline-build-service && strip /workspace/target/release/pipeline-build-service

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/pipeline-build-service

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3 python3

ENV HOST=0.0.0.0
ENV PORT=50081

COPY --from=builder /workspace/target/release/pipeline-build-service /usr/local/bin/pipeline-build-service
COPY --from=builder /workspace/services/pipeline-build-service/config ./config

EXPOSE 50081
CMD ["/usr/local/bin/pipeline-build-service"]
