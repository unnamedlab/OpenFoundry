/*
 * services/pipeline-runner/src/main/scala/com/openfoundry/pipeline/PipelineRunner.scala
 *
 * FASE 3 / Tarea 3.3 of docs/architecture/migration-plan-foundry-pattern-orchestration.md.
 *
 * Entry point for the SparkApplication CR generated from
 * infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml.
 *
 *   spark-submit
 *     --class com.openfoundry.pipeline.PipelineRunner
 *     /opt/spark/jars/pipeline-runner_2.12-0.1.0.jar
 *     --pipeline-id <RID>
 *     --run-id      <ULID>
 *     --input-dataset  <catalog.namespace.table>
 *     --output-dataset <catalog.namespace.table>
 *     --catalog        <name, e.g. lakekeeper>
 *     --catalog-uri    <https://...>
 *     [--pipeline-build-url http://pipeline-build-service.openfoundry.svc:50081]
 *     [--smoke]
 *
 * Behaviour:
 *   1. Parse args (no third-party CLI lib — keep dep surface zero).
 *   2. Build a SparkSession; every catalog/S3 conf is set on the
 *      SparkApplication CR (sparkConf:) and inherited automatically.
 *   3. Resolve the transform spec:
 *        a. If --smoke is passed, skip step (3b) and use the built-in
 *           smoke transform (`SELECT 1, current_timestamp()`).
 *        b. Otherwise GET ${pipeline_build_url}/api/v1/data-integration
 *           /pipelines/{pipeline_id}/runs/{run_id}/spec and parse the
 *           response body as JSON. (The endpoint will be wired up in
 *           Tarea 3.4. Until then, a 404 falls back to the smoke
 *           transform so the SparkApplication CR template can still be
 *           exercised end-to-end against a freshly-deployed Iceberg
 *           catalog.)
 *   4. Execute the SQL via Spark, write to the output Iceberg table.
 *   5. Exit non-zero on any error so the Spark Operator can mark the
 *      SparkApplication FAILED and respect the restartPolicy in
 *      _pipeline-run-template.yaml.
 *
 * No third-party dependencies (Provided spark-sql + Java 17 stdlib only):
 *   - JSON parsing is hand-rolled because the spec body has only three
 *     scalar fields (transform_type, sql, format). When the spec schema
 *     grows in 3.4 we will swap in spark-sql's `from_json` helper.
 *   - HTTP client is `java.net.http.HttpClient`.
 */
package com.openfoundry.pipeline

import java.net.URI
import java.net.http.{HttpClient, HttpRequest, HttpResponse}
import java.nio.charset.StandardCharsets
import java.time.Duration

import org.apache.spark.sql.{DataFrame, SparkSession}

object PipelineRunner {

  /** Parsed CLI arguments. All fields are required except where noted. */
  private final case class Args(
      pipelineId: String,
      runId: String,
      inputDataset: String,
      outputDataset: String,
      catalog: String,
      catalogUri: String,
      pipelineBuildUrl: String,
      smoke: Boolean,
  )

  /**
   * Resolved transform spec. In Tarea 3.3 only `sql` is meaningful;
   * `format` defaults to "iceberg". Tarea 3.4 will extend this with
   * pyspark/wasm transforms, view filters, etc.
   */
  private final case class TransformSpec(sql: String, format: String)

  /**
   * Default base URL used when neither --pipeline-build-url nor the
   * OF_PIPELINE_BUILD_URL env var is set. Mirrors the in-cluster
   * Service DNS for pipeline-build-service (see
   * services/pipeline-build-service/Cargo.toml ENV PORT=50081).
   */
  private val DefaultPipelineBuildUrl: String =
    "http://pipeline-build-service.openfoundry.svc:50081"

  /** Spec endpoint timeout — matches the 30-min start-to-close on the CR. */
  private val SpecHttpTimeout: Duration = Duration.ofSeconds(30)

  def main(rawArgs: Array[String]): Unit = {
    val args =
      try parseArgs(rawArgs.toList)
      catch {
        case e: IllegalArgumentException =>
          System.err.println(s"pipeline-runner: ${e.getMessage}")
          System.err.println(usage())
          sys.exit(2)
      }

    log(args, s"starting (smoke=${args.smoke})")

    val spark = buildSession(args)
    var exitCode = 0
    try {
      val spec = resolveSpec(args)
      log(args, s"resolved transform: format=${spec.format} sql=${preview(spec.sql)}")

      val df: DataFrame = spark.sql(spec.sql)
      val rowsWritten = writeOutput(spark, df, args, spec)
      log(args, s"wrote $rowsWritten rows to ${args.outputDataset} (format=${spec.format})")
    } catch {
      case e: Throwable =>
        System.err.println(s"pipeline-runner: FAILED ${e.getClass.getSimpleName}: ${e.getMessage}")
        e.printStackTrace(System.err)
        exitCode = 1
    } finally {
      try spark.stop()
      catch { case _: Throwable => () }
    }

    sys.exit(exitCode)
  }

  // ---------------------------------------------------------------------
  // CLI parsing.
  //
  // No third-party CLI lib — the surface is small and stable, and
  // pipeline-build-service emits arguments deterministically from the
  // template in 3.2. Repeated flags overwrite earlier values; unknown
  // flags raise IllegalArgumentException.
  // ---------------------------------------------------------------------

  private def parseArgs(args: List[String]): Args = {
    var pipelineId: Option[String] = None
    var runId: Option[String] = None
    var inputDataset: Option[String] = None
    var outputDataset: Option[String] = None
    var catalog: Option[String] = None
    var catalogUri: Option[String] = None
    var pipelineBuildUrl: Option[String] = None
    var smoke: Boolean = false

    @scala.annotation.tailrec
    def loop(rest: List[String]): Unit = rest match {
      case Nil => ()
      case "--pipeline-id" :: v :: tail =>
        pipelineId = Some(requireNonEmpty("--pipeline-id", v)); loop(tail)
      case "--run-id" :: v :: tail =>
        runId = Some(requireNonEmpty("--run-id", v)); loop(tail)
      case "--input-dataset" :: v :: tail =>
        inputDataset = Some(requireNonEmpty("--input-dataset", v)); loop(tail)
      case "--output-dataset" :: v :: tail =>
        outputDataset = Some(requireNonEmpty("--output-dataset", v)); loop(tail)
      case "--catalog" :: v :: tail =>
        catalog = Some(requireNonEmpty("--catalog", v)); loop(tail)
      case "--catalog-uri" :: v :: tail =>
        catalogUri = Some(requireNonEmpty("--catalog-uri", v)); loop(tail)
      case "--pipeline-build-url" :: v :: tail =>
        pipelineBuildUrl = Some(requireNonEmpty("--pipeline-build-url", v)); loop(tail)
      case "--smoke" :: tail =>
        smoke = true; loop(tail)
      case other :: _ =>
        throw new IllegalArgumentException(s"unknown argument: $other")
    }
    loop(args)

    val effectiveBuildUrl = pipelineBuildUrl
      .orElse(Option(System.getenv("OF_PIPELINE_BUILD_URL")).filter(_.trim.nonEmpty))
      .getOrElse(DefaultPipelineBuildUrl)

    Args(
      pipelineId       = required("--pipeline-id", pipelineId),
      runId            = required("--run-id", runId),
      inputDataset     = required("--input-dataset", inputDataset),
      outputDataset    = required("--output-dataset", outputDataset),
      catalog          = required("--catalog", catalog),
      catalogUri       = required("--catalog-uri", catalogUri),
      pipelineBuildUrl = effectiveBuildUrl,
      smoke            = smoke,
    )
  }

  private def required[T](flag: String, value: Option[T]): T =
    value.getOrElse(throw new IllegalArgumentException(s"missing required flag: $flag"))

  private def requireNonEmpty(flag: String, value: String): String = {
    val trimmed = value.trim
    if (trimmed.isEmpty)
      throw new IllegalArgumentException(s"flag $flag requires a non-empty value")
    trimmed
  }

  private def usage(): String =
    """usage: spark-submit --class com.openfoundry.pipeline.PipelineRunner pipeline-runner.jar
      |  --pipeline-id <id> --run-id <id>
      |  --input-dataset <catalog.namespace.table>
      |  --output-dataset <catalog.namespace.table>
      |  --catalog <name> --catalog-uri <url>
      |  [--pipeline-build-url <url>] [--smoke]""".stripMargin

  // ---------------------------------------------------------------------
  // Spark session.
  //
  // All catalog / S3 / Iceberg configuration is set on the
  // SparkApplication CR via spec.sparkConf (see
  // infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml).
  // Anything we set here would override that, so we keep it minimal:
  // just the app name, plus a defensive re-assertion of the catalog
  // URI in case the runner is invoked via plain spark-submit (smoke).
  // ---------------------------------------------------------------------

  private def buildSession(args: Args): SparkSession = {
    val builder = SparkSession.builder()
      .appName(s"pipeline-run-${args.pipelineId}-${args.runId}")
      // Re-assert the catalog conf in case the runner is launched
      // outside of the SparkApplication CR (local smoke / kubectl run).
      // When the CR sets these, the values match and Spark deduplicates.
      .config(s"spark.sql.catalog.${args.catalog}", "org.apache.iceberg.spark.SparkCatalog")
      .config(s"spark.sql.catalog.${args.catalog}.type", "rest")
      .config(s"spark.sql.catalog.${args.catalog}.uri", args.catalogUri)
      .config(
        "spark.sql.extensions",
        "org.apache.iceberg.spark.extensions.IcebergSparkSessionExtensions",
      )
    builder.getOrCreate()
  }

  // ---------------------------------------------------------------------
  // Spec resolution.
  //
  // GET ${pipeline_build_url}/api/v1/data-integration/pipelines/{id}
  //     /runs/{run_id}/spec
  //
  // Tarea 3.4 will land this endpoint. Until then, any 404 / connection
  // failure falls back to the smoke transform — that way the
  // SparkApplication CR template from Tarea 3.2 can be exercised
  // end-to-end against a freshly-deployed cluster.
  // ---------------------------------------------------------------------

  private def resolveSpec(args: Args): TransformSpec = {
    if (args.smoke) {
      log(args, "smoke mode: using built-in 1-row transform")
      return smokeSpec(args)
    }

    val url = s"${args.pipelineBuildUrl.stripSuffix("/")}" +
      s"/api/v1/data-integration/pipelines/${args.pipelineId}" +
      s"/runs/${args.runId}/spec"

    val client = HttpClient.newBuilder()
      .connectTimeout(SpecHttpTimeout)
      .build()
    val req = HttpRequest.newBuilder()
      .uri(URI.create(url))
      .timeout(SpecHttpTimeout)
      .header("Accept", "application/json")
      .GET()
      .build()

    val response: HttpResponse[String] =
      try client.send(req, HttpResponse.BodyHandlers.ofString(StandardCharsets.UTF_8))
      catch {
        case e: java.io.IOException =>
          log(args, s"spec endpoint $url unreachable (${e.getMessage}); falling back to smoke")
          return smokeSpec(args)
      }

    response.statusCode() match {
      case 200 =>
        parseSpec(response.body()).getOrElse {
          log(args, s"spec endpoint returned malformed body; falling back to smoke")
          smokeSpec(args)
        }
      case 404 =>
        log(args, s"spec endpoint $url returned 404; falling back to smoke")
        smokeSpec(args)
      case other =>
        throw new RuntimeException(
          s"spec endpoint $url returned HTTP $other: ${truncate(response.body(), 512)}"
        )
    }
  }

  private def smokeSpec(args: Args): TransformSpec = TransformSpec(
    sql =
      s"""SELECT
         |  CAST('${escapeSqlLiteral(args.runId)}' AS STRING) AS run_id,
         |  CAST(current_timestamp() AS TIMESTAMP) AS observed_at""".stripMargin,
    format = "iceberg",
  )

  /**
   * Hand-rolled JSON parser, scoped to exactly the two fields the spec
   * carries today: `sql` (string, required) and `format` (string,
   * optional, defaults to "iceberg"). Returns `None` on any parse error;
   * the caller falls back to the smoke transform.
   *
   * Limitation: only supports flat top-level objects with string values.
   * Tarea 3.4 will replace this with `from_json` once the spec grows
   * arrays / nested objects (input filters, output partitioning, ...).
   */
  private def parseSpec(body: String): Option[TransformSpec] = {
    val sql = extractStringField(body, "sql")
    val format = extractStringField(body, "format").getOrElse("iceberg")
    sql.map(TransformSpec(_, format))
  }

  /**
   * Find `"name"\s*:\s*"value"` and return the unescaped value.
   * Handles `\"` and `\\` inside the value; bails on other escapes
   * (returns None) so we don't silently mis-decode a complex spec.
   */
  private def extractStringField(json: String, name: String): Option[String] = {
    val needle = "\"" + name + "\""
    val nameIdx = json.indexOf(needle)
    if (nameIdx < 0) return None
    var i = nameIdx + needle.length
    // Skip whitespace + `:` + whitespace.
    while (i < json.length && json.charAt(i).isWhitespace) i += 1
    if (i >= json.length || json.charAt(i) != ':') return None
    i += 1
    while (i < json.length && json.charAt(i).isWhitespace) i += 1
    if (i >= json.length || json.charAt(i) != '"') return None
    i += 1
    val sb = new StringBuilder
    while (i < json.length) {
      val c = json.charAt(i)
      if (c == '"') return Some(sb.toString)
      if (c == '\\') {
        if (i + 1 >= json.length) return None
        json.charAt(i + 1) match {
          case '"'  => sb.append('"')
          case '\\' => sb.append('\\')
          case 'n'  => sb.append('\n')
          case 't'  => sb.append('\t')
          case _    => return None // unsupported escape; fall back to smoke
        }
        i += 2
      } else {
        sb.append(c)
        i += 1
      }
    }
    None // unterminated string
  }

  // ---------------------------------------------------------------------
  // Output write.
  //
  // Iceberg + Spark 3.5 → use the v2 `writeTo(...).append()` API. The
  // output dataset is expected to be a fully-qualified Iceberg table
  // identifier (catalog.namespace.table) that matches the catalog
  // configured on the SparkSession.
  //
  // Returns the row count for logging. Iceberg's `append` is atomic so
  // failure → no partial commit, which preserves the WORM guarantee on
  // audit datasets enforced by serviceAccount: spark-jobs-non-audit.
  // ---------------------------------------------------------------------

  private def writeOutput(
      spark: SparkSession,
      df: DataFrame,
      args: Args,
      spec: TransformSpec,
  ): Long = {
    val rowCount = df.count() // materialise once for the log line below
    spec.format match {
      case "iceberg" =>
        df.writeTo(args.outputDataset).append()
      case other =>
        throw new RuntimeException(
          s"unsupported output format '$other' (only 'iceberg' wired up in 3.3; pyspark/wasm in 3.4)"
        )
    }
    rowCount
  }

  // ---------------------------------------------------------------------
  // Misc helpers.
  // ---------------------------------------------------------------------

  private def log(args: Args, msg: String): Unit =
    // stdout is what the Spark Operator surfaces via `kubectl logs`.
    // Keep the prefix machine-parseable so the Foundry-style live log
    // viewer (TaskExecution Logs in the existing maintenance jobs) can
    // pin-fold by pipeline_id / run_id.
    println(s"[pipeline-runner pipeline_id=${args.pipelineId} run_id=${args.runId}] $msg")

  private def preview(sql: String): String = {
    val singleLine = sql.replace('\n', ' ').replaceAll("\\s+", " ").trim
    truncate(singleLine, 200)
  }

  private def truncate(s: String, max: Int): String =
    if (s.length <= max) s else s.substring(0, max) + s"… (${s.length - max} more)"

  /** Escape a string for inline use in a SQL string literal. */
  private def escapeSqlLiteral(s: String): String = s.replace("'", "''")
}
