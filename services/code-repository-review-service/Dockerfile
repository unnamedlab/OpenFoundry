FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p code-repository-review-service && strip /workspace/target/release/code-repository-review-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50155

COPY --from=builder /workspace/target/release/code-repository-review-service /usr/local/bin/code-repository-review-service

EXPOSE 50155
CMD ["/usr/local/bin/code-repository-review-service"]
