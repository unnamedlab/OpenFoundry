// pipeline-runner-spark — Scala entrypoint that the Go orchestrator drives via
// `spark-submit --class com.openfoundry.pipeline.PipelineRunner`.
//
// Contract (set in lockstep with services/pipeline-runner/internal/runner/run.go):
//   --pipeline-id     <string>    pipeline RID, used for log scoping
//   --run-id          <string>    per-run ULID
//   --input-dataset   <string>    Iceberg table reference (catalog.namespace.table)
//   --output-dataset  <string>    Iceberg table reference
//   --catalog         <string>    Spark catalog name, e.g. "lakekeeper"
//   --catalog-uri     <string>    Lakekeeper REST URL
//   --inline-sql      <string>    SQL body to execute (input/output already
//                                 resolved by the Go orchestrator)
//   --inline-format   <string>    Output format hint (currently only "iceberg")
//   --smoke                       Dry-run: skip execution after spec validation
//
// Behaviour:
//   1. Build SparkSession with the Iceberg REST catalog wired (matches
//      infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml).
//   2. Run `--inline-sql` (the resolved SELECT or aggregate the Go service has
//      composed from the pipeline DAG node config).
//   3. Persist the result via `df.writeTo(outputDataset).createOrReplace()` so
//      the Iceberg snapshot is published atomically.
//   4. Emit machine-parseable log lines that mirror the Go orchestrator's
//      `[pipeline-runner pipeline_id=… run_id=…] …` prefix.
//
// Failure modes surface as non-zero exit codes; spark-submit propagates them
// to the SparkApplication CR, which Spark Operator marks FAILED, which the
// pipeline-build-service watch then reports as a runtime_error to the caller.
package com.openfoundry.pipeline

import org.apache.spark.sql.SparkSession
import scopt.OParser

import scala.util.{Failure, Success, Try}

final case class RunnerArgs(
  pipelineId: String   = "",
  runId: String        = "",
  inputDataset: String = "",
  outputDataset: String = "",
  catalog: String      = "lakekeeper",
  catalogUri: String   = "",
  inlineSql: String    = "",
  inlineFormat: String = "iceberg",
  smoke: Boolean       = false,
)

object PipelineRunner {

  private val builder = OParser.builder[RunnerArgs]
  private val parser = {
    import builder._
    OParser.sequence(
      programName("pipeline-runner-spark"),
      head("pipeline-runner-spark", sys.env.getOrElse("VERSION", "dev")),
      opt[String]("pipeline-id").required().action((v, a) => a.copy(pipelineId = v)),
      opt[String]("run-id").required().action((v, a) => a.copy(runId = v)),
      opt[String]("input-dataset").action((v, a) => a.copy(inputDataset = v)),
      opt[String]("output-dataset").required().action((v, a) => a.copy(outputDataset = v)),
      opt[String]("catalog").action((v, a) => a.copy(catalog = v)),
      opt[String]("catalog-uri").action((v, a) => a.copy(catalogUri = v)),
      opt[String]("inline-sql").action((v, a) => a.copy(inlineSql = v)),
      opt[String]("inline-format").action((v, a) => a.copy(inlineFormat = v)),
      opt[Unit]("smoke").action((_, a) => a.copy(smoke = true)),
    )
  }

  def main(rawArgs: Array[String]): Unit = {
    val parsed = OParser.parse(parser, rawArgs, RunnerArgs())
    val args = parsed.getOrElse {
      System.err.println("[pipeline-runner-spark] failed to parse arguments")
      sys.exit(2)
    }

    log(args, s"resolved args: input=${args.inputDataset} output=${args.outputDataset} catalog=${args.catalog} sql=${preview(args.inlineSql)}")

    if (args.smoke) {
      log(args, "smoke mode: skipping spark execution")
      sys.exit(0)
    }

    val spark = buildSession(args)
    try {
      Try(runTransform(spark, args)) match {
        case Success(rowCount) =>
          log(args, s"transform completed rows=$rowCount output=${args.outputDataset}")
        case Failure(err) =>
          log(args, s"transform failed: ${err.getClass.getSimpleName}: ${err.getMessage}")
          err.printStackTrace(System.err)
          sys.exit(1)
      }
    } finally {
      spark.stop()
    }
  }

  /** Build a SparkSession with the OpenFoundry Iceberg catalog wired. The S3
   *  endpoint, region and credentials are read from spark.hadoop.fs.s3a.* — the
   *  CR template seeds the endpoint, the operator injects the credentials env
   *  vars, and the pipeline-build-service ApplicationBuilder picks the bucket
   *  per pipeline. */
  private def buildSession(args: RunnerArgs): SparkSession = {
    val builder = SparkSession
      .builder()
      .appName(s"pipeline-${args.pipelineId}-${args.runId}")
      .config("spark.sql.extensions", "org.apache.iceberg.spark.extensions.IcebergSparkSessionExtensions")
      .config(s"spark.sql.catalog.${args.catalog}", "org.apache.iceberg.spark.SparkCatalog")
      .config(s"spark.sql.catalog.${args.catalog}.type", "rest")

    val withCatalog = if (args.catalogUri.nonEmpty) {
      builder.config(s"spark.sql.catalog.${args.catalog}.uri", args.catalogUri)
    } else {
      builder
    }

    withCatalog.getOrCreate()
  }

  /** Execute the inline SQL and persist via writeTo(...).createOrReplace() so
   *  Iceberg publishes a single atomic snapshot. */
  private def runTransform(spark: SparkSession, args: RunnerArgs): Long = {
    val sql = Option(args.inlineSql).getOrElse("").trim
    if (sql.isEmpty) {
      throw new IllegalArgumentException("--inline-sql is empty; nothing to execute")
    }

    val df = spark.sql(sql)
    log(args, s"executed sql, schema=${df.schema.simpleString}")

    args.inlineFormat match {
      case "iceberg" =>
        df.writeTo(args.outputDataset).createOrReplace()
        log(args, s"wrote iceberg table ${args.outputDataset}")
      case other =>
        throw new IllegalArgumentException(s"unsupported --inline-format: $other")
    }

    df.count()
  }

  private def log(args: RunnerArgs, msg: String): Unit =
    println(s"[pipeline-runner-spark pipeline_id=${args.pipelineId} run_id=${args.runId}] $msg")

  private def preview(s: String): String = {
    if (s == null) return ""
    val flat = s.replace("\n", " ").replaceAll("\\s+", " ").trim
    if (flat.length <= 200) flat else flat.take(200) + s"… (${flat.length - 200} more)"
  }
}
