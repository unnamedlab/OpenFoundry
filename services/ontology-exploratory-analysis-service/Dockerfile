FROM rust:1.95-alpine3.23 AS builder
WORKDIR /workspace

RUN apk add --no-cache build-base pkgconf openssl-dev ca-certificates

COPY . .
RUN cargo build --locked --release -p ontology-exploratory-analysis-service && strip /workspace/target/release/ontology-exploratory-analysis-service

FROM alpine:3.23 AS runtime
WORKDIR /app

RUN apk add --no-cache ca-certificates libgcc libstdc++ libssl3

ENV HOST=0.0.0.0
ENV PORT=50131

COPY --from=builder /workspace/target/release/ontology-exploratory-analysis-service /usr/local/bin/ontology-exploratory-analysis-service

EXPOSE 50131
CMD ["/usr/local/bin/ontology-exploratory-analysis-service"]
