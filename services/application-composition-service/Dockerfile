FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p application-composition-service && strip /workspace/target/release/application-composition-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50140

COPY --from=builder /workspace/target/release/application-composition-service /usr/local/bin/application-composition-service

EXPOSE 50140
CMD ["/usr/local/bin/application-composition-service"]
