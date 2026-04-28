FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p solution-design-service && strip /workspace/target/release/solution-design-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50142

COPY --from=builder /workspace/target/release/solution-design-service /usr/local/bin/solution-design-service

EXPOSE 50142
CMD ["/usr/local/bin/solution-design-service"]
