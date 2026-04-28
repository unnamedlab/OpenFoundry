FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p identity-federation-service && strip /workspace/target/release/identity-federation-service

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/identity-federation-service

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50051

COPY --from=builder /workspace/target/release/identity-federation-service /usr/local/bin/identity-federation-service
COPY --from=builder /workspace/services/identity-federation-service/config ./config

EXPOSE 50112
CMD ["/usr/local/bin/identity-federation-service"]
