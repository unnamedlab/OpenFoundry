FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p workflow-automation-service && strip /workspace/target/release/workflow-automation-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50137

COPY --from=builder /workspace/target/release/workflow-automation-service /usr/local/bin/workflow-automation-service

EXPOSE 50137
CMD ["/usr/local/bin/workflow-automation-service"]
