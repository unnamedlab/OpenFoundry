FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p tenancy-organizations-service && strip /workspace/target/release/tenancy-organizations-service

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/tenancy-organizations-service

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50113

COPY --from=builder /workspace/target/release/tenancy-organizations-service /usr/local/bin/tenancy-organizations-service
COPY --from=builder /workspace/services/tenancy-organizations-service/config ./config

EXPOSE 50113
CMD ["/usr/local/bin/tenancy-organizations-service"]
