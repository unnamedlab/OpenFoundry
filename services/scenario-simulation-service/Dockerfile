FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p scenario-simulation-service && strip /workspace/target/release/scenario-simulation-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50141

COPY --from=builder /workspace/target/release/scenario-simulation-service /usr/local/bin/scenario-simulation-service

EXPOSE 50141
CMD ["/usr/local/bin/scenario-simulation-service"]
