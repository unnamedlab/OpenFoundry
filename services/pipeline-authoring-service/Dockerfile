FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev python3-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p pipeline-authoring-service && strip /workspace/target/release/pipeline-authoring-service

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/pipeline-authoring-service

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3 python3

ENV HOST=0.0.0.0
ENV PORT=50080

COPY --from=builder /workspace/target/release/pipeline-authoring-service /usr/local/bin/pipeline-authoring-service
COPY --from=builder /workspace/services/pipeline-authoring-service/config ./config

EXPOSE 50080
CMD ["/usr/local/bin/pipeline-authoring-service"]
