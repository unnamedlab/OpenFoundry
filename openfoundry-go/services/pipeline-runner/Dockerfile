# syntax=docker/dockerfile:1.7
#
# Two-stage image: Go binary + the upstream apache/spark base so the
# orchestrator can shell out to /opt/spark/bin/spark-submit. Final
# size = base (~600 MB) + ~15 MB Go binary + the Iceberg JARs.

FROM golang:1.25 AS go-builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/pipeline-runner \
      ./services/pipeline-runner/cmd/pipeline-runner

FROM apache/spark:3.5.4-scala2.12-java17-python3-ubuntu

# Iceberg + AWS bundle JARs — the runtime contract is unchanged from
# the Scala module, so the maintenance jobs and SparkApplication CR
# template do not need to be re-rendered.
ARG ICEBERG_VERSION=1.5.2
ADD https://repo1.maven.org/maven2/org/apache/iceberg/iceberg-spark-runtime-3.5_2.12/${ICEBERG_VERSION}/iceberg-spark-runtime-3.5_2.12-${ICEBERG_VERSION}.jar /opt/spark/jars/
ADD https://repo1.maven.org/maven2/org/apache/iceberg/iceberg-aws-bundle/${ICEBERG_VERSION}/iceberg-aws-bundle-${ICEBERG_VERSION}.jar /opt/spark/jars/

# Scala JAR holding the Spark-side df.writeTo(...).append() pathway.
# Built separately in the openfoundry-spark repo and dropped in via
# Helm values.yaml during image promotion.
COPY --from=go-builder /out/pipeline-runner /usr/local/bin/pipeline-runner

USER 185:185
ENTRYPOINT ["/usr/local/bin/pipeline-runner"]
