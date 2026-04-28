FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p sql-warehousing-service && strip /workspace/target/release/sql-warehousing-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50123

COPY --from=builder /workspace/target/release/sql-warehousing-service /usr/local/bin/sql-warehousing-service

EXPOSE 50123
CMD ["/usr/local/bin/sql-warehousing-service"]
