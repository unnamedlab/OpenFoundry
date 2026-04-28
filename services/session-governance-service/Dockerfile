FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p session-governance-service && strip /workspace/target/release/session-governance-service

FROM alpine:3.23 AS runtime
WORKDIR /workspace/services/session-governance-service

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50074

COPY --from=builder /workspace/target/release/session-governance-service /usr/local/bin/session-governance-service
COPY --from=builder /workspace/services/session-governance-service/config ./config

EXPOSE 50074
CMD ["/usr/local/bin/session-governance-service"]

