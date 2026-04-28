FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p analytical-logic-service && strip /workspace/target/release/analytical-logic-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50136

COPY --from=builder /workspace/target/release/analytical-logic-service /usr/local/bin/analytical-logic-service

EXPOSE 50136
CMD ["/usr/local/bin/analytical-logic-service"]
