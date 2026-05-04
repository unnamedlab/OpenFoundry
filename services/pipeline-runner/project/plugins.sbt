// Intentionally empty.
//
// We do NOT use sbt-assembly to build a fat jar: the runtime image
// already ships spark-sql + iceberg-spark-runtime + iceberg-aws-bundle
// in /opt/spark/jars/. The output of `sbt package` is therefore a
// ~10 KB jar that only contains com.openfoundry.pipeline.* classes.
//
// Add plugins here only when strictly necessary, and document why.
