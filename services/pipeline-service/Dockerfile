FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev python3-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p pipeline-service && strip /workspace/target/release/pipeline-service

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/pipeline-service

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3 python3

ENV HOST=0.0.0.0
ENV PORT=50056

COPY --from=builder /workspace/target/release/pipeline-service /usr/local/bin/pipeline-service
COPY --from=builder /workspace/services/pipeline-service/config ./config

EXPOSE 50056
CMD ["/usr/local/bin/pipeline-service"]
